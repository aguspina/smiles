package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"smiles/model"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/schollz/progressbar/v3"
)

const (
	readFromFile            = false
	useCommandLineArguments = true
	mockResponseFilePath    = "data/response.json"
	dateLayout              = "2006-01-02"
	cabinType               = "all"
	maxStops                = 1
	bigMaxMilesNumber       = 9_999_999
)

func main() {

	var daysToQuery int
	var departureDate string
	var returnDate string
	scanner := bufio.NewScanner(os.Stdin)

	fmt.Print("Ingrese el número de días a consultar: ")
	scanner.Scan()
	daysToQuery, _ = strconv.Atoi(scanner.Text())

	fmt.Print("Ingrese la fecha de salida (YYYY-MM-DD): ")
	scanner.Scan()
	departureDate = scanner.Text()

	fmt.Print("Ingrese la fecha de regreso (YYYY-MM-DD): ")
	scanner.Scan()
	returnDate = scanner.Text()
	// Define los origines y destinos a buscar
	fmt.Print("Ingrese los origenes separados por comas o espacios: ")
	scanner.Scan()
	input := scanner.Text()

	// Dividir la entrada en elementos individuales
	origins := strings.FieldsFunc(input, func(c rune) bool {
		return c == ' ' || c == ','
	})

	fmt.Print("Ingrese los destinos separados por comas o espacios: ")
	scanner.Scan()
	input = scanner.Text()

	// Dividir la entrada en elementos individuales
	destinations := strings.FieldsFunc(input, func(c rune) bool {
		return c == ' ' || c == ','
	})

	c := http.Client{}

	startingDepartureDate, err := time.Parse(dateLayout, departureDate)
	startingReturningDate, err := time.Parse(dateLayout, returnDate)
	if err != nil {
		log.Fatal("Error parsing starting date")
	}

	departuresCh := make(chan model.Result, daysToQuery*len(origins)*len(destinations))
	returnsCh := make(chan model.Result, daysToQuery*len(origins)*len(destinations))

	bar := progressbar.NewOptions(int(int64(daysToQuery)*int64(len(origins))*int64(len(destinations)))*2,
		progressbar.OptionSetDescription("Consultando vuelos en las fechas y tramos seleccionados.."),
		progressbar.OptionSetWidth(40),
		progressbar.OptionSetRenderBlankState(true),
	)

	fmt.Println()
	start := time.Now()
	var wg sync.WaitGroup

	for _, origin := range origins {
		for _, destination := range destinations {
			fmt.Printf("Desde: %s\n", origin)
			fmt.Printf("Hasta: %s\n", destination)
			for i := 0; i < daysToQuery; i++ {
				departureDate := startingDepartureDate.AddDate(0, 0, i)
				returnDate := startingReturningDate.AddDate(0, 0, i)

				wg.Add(2)
				go makeRequest(&wg, departuresCh, &c, departureDate, origin, destination, bar)
				// inverting airports and changing date to query returns
				go makeRequest(&wg, returnsCh, &c, returnDate, destination, origin, bar)
			}
		}
	}

	wg.Wait()
	close(departuresCh)
	close(returnsCh)

	elapsed := time.Since(start).Round(time.Second).String()
	fmt.Printf("\nLas consultas tomaron %s\n", elapsed)

	var departureResults []model.Result
	var returnResults []model.Result

	for elem := range departuresCh {
		departureResults = append(departureResults, elem)
	}

	for elem := range returnsCh {
		returnResults = append(returnResults, elem)
	}

	sortResults(departureResults)
	sortResults(returnResults)

	fmt.Println("VUELOS DE IDA")
	processResults(&c, departureResults)

	fmt.Println("VUELOS DE VUELTA")
	processResults(&c, returnResults)
}

func sortResults(r []model.Result) {
	sort.Slice(r, func(i, j int) bool {
		return r[i].QueryDate.Before(r[j].QueryDate)
	})
}

func makeRequest(wg *sync.WaitGroup, ch chan<- model.Result, c *http.Client, startingDate time.Time, originAirport string, destinationAirport string, bar *progressbar.ProgressBar) {
	defer wg.Done()
	defer bar.Add(1)

	var body []byte
	var err error
	data := model.Data{}

	u := createURL(startingDate.Format(dateLayout), originAirport, destinationAirport) // Encode and assign back to the original query.
	req := createRequest(u, "api-air-flightsearch-prd.smiles.com.br")

	res, err := c.Do(req)
	if err != nil {
		log.Fatal("Error making request ", err)
	}

	body, err = ioutil.ReadAll(res.Body)
	if body == nil {
		log.Fatal("Empty result")
	}

	if err := json.Unmarshal(body, &data); err != nil {
		log.Fatal("Error unmarshalling data ", err)
	}

	ch <- model.Result{Data: data, QueryDate: startingDate}
}

func createRequest(u url.URL, authority string) *http.Request {
	req, err := http.NewRequest("GET", u.String(), nil)
	if err != nil {
		log.Fatal("Error creating request ", err)
	}

	// headers
	req.Header.Add("x-api-key", "aJqPU7xNHl9qN3NVZnPaJ208aPo2Bh2p2ZV844tw")
	req.Header.Add("region", "ARGENTINA")
	req.Header.Add("origin", "https://www.smiles.com.ar")
	req.Header.Add("referer", "https://www.smiles.com.ar")
	req.Header.Add("channel", "web")
	req.Header.Add("authority", authority)
	return req
}

func createURL(departureDate string, originAirport string, destinationAirport string) url.URL {
	u := url.URL{
		Scheme:   "https",
		Host:     "api-air-flightsearch-prd.smiles.com.br",
		RawQuery: "adults=1&children=0&currencyCode=ARS&infants=0&isFlexibleDateChecked=false&tripType=2&forceCongener=true&r=ar",
		Path:     "/v1/airlines/search",
	}
	q := u.Query()
	q.Add("departureDate", departureDate)
	q.Add("originAirportCode", originAirport)
	q.Add("destinationAirportCode", destinationAirport)
	q.Add("cabinType", cabinType)
	u.RawQuery = q.Encode()
	return u
}

func createTaxURL(departureFlight *model.Flight, departureFare *model.Fare) url.URL {
	u := url.URL{
		Scheme:   "https",
		Host:     "api-airlines-boarding-tax-prd.smiles.com.br",
		RawQuery: "adults=1&children=0&infants=0&highlightText=SMILES_CLUB",
		Path:     "/v1/airlines/flight/boardingtax",
	}
	q := u.Query()
	q.Add("type", "SEGMENT_1")
	q.Add("uid", departureFlight.UId)
	q.Add("fareuid", departureFare.UId)
	u.RawQuery = q.Encode()
	return u
}

func getSmilesClubFare(f *model.Flight) *model.Fare {
	for i, v := range f.FareList {
		if v.FType == "SMILES_CLUB" {
			return &f.FareList[i]
		}
	}
	fmt.Println("WARN: SMILES_CLUB fare not fund")
	// for the sake of simplicity returning ridiculous default big number when fare not found
	return &model.Fare{Miles: bigMaxMilesNumber}
}

func processResults(c *http.Client, r []model.Result) {
	// using the first flight as cheapest default
	var cheapestFlight model.Flight
	cheapestFare := &model.Fare{
		Miles: bigMaxMilesNumber,
	}

	// loop through all results
	for _, v := range r {
		var cheapestFlightDay model.Flight
		cheapestFareDay := &model.Fare{
			Miles: bigMaxMilesNumber,
		}

		// loop through all flights by day
		if len(v.Data.RequestedFlightSegmentList) > 0 {
			for _, f := range v.Data.RequestedFlightSegmentList[0].FlightList {
				smilesClubFare := getSmilesClubFare(&f)
				if cheapestFareDay.Miles > smilesClubFare.Miles {
					cheapestFlightDay = f
					cheapestFareDay = smilesClubFare
				}
			}
		}

		if cheapestFare.Miles > cheapestFareDay.Miles {
			cheapestFlight = cheapestFlightDay
			cheapestFare = cheapestFareDay
		}

		if cheapestFareDay.Miles != bigMaxMilesNumber {
			fmt.Printf("Vuelo más barato del día %s: %s - %s, %s, %s, %d escalas, %d millas\n",
				cheapestFlightDay.Departure.Date.Format(dateLayout),
				cheapestFlightDay.Departure.Airport.Code,
				cheapestFlightDay.Arrival.Airport.Code,
				cheapestFlightDay.Cabin,
				cheapestFlightDay.Airline.Name,
				cheapestFlightDay.Stops,
				cheapestFareDay.Miles,
			)
		}
	}

	fmt.Println()
	if cheapestFare.Miles != bigMaxMilesNumber {
		boardingTax := getTaxForFlight(c, &cheapestFlight, cheapestFare)

		fmt.Printf("Vuelo más barato en estas fecha: %s",
			cheapestFlight.Departure.Date.Format(dateLayout),
		)
		fmt.Println()
		fmt.Printf("AEROPUERTOS: %s - %s",
			cheapestFlight.Departure.Airport.Code,
			cheapestFlight.Arrival.Airport.Code,
		)
		fmt.Println()
		fmt.Printf("AVION: %s, %s, %d escalas",
			cheapestFlight.Cabin,
			cheapestFlight.Airline.Name,
			cheapestFlight.Stops,
		)
		fmt.Println()
		fmt.Printf("PRECIO: %d millas, %d millas totales aprox, ARS %f\n",
			cheapestFare.Miles,
			boardingTax.Totals.Total.Miles,
			float64(cheapestFare.Miles)*1.4+float64(boardingTax.Totals.Total.Money),
		)

	}
	fmt.Println()
}

func getTaxForFlight(c *http.Client, flight *model.Flight, fare *model.Fare) *model.BoardingTax {
	u := createTaxURL(flight, fare)
	r := createRequest(u, "api-airlines-boarding-tax-prd.smiles.com.br")
	var body []byte
	var data model.BoardingTax

	res, err := c.Do(r)
	if err != nil {
		log.Fatal("Error making request ", err)
	}

	body, err = ioutil.ReadAll(res.Body)
	if body == nil {
		log.Fatal("Empty result")
	}

	if err := json.Unmarshal(body, &data); err != nil {
		log.Fatal("Error unmarshalling data ", err)
	}

	return &data
}

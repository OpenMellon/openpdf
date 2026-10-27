package timestamp_test

import (
	"bytes"
	"crypto"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"strings"

	"github.com/OpenMellon/openpdf/timestamp"
)

// ExampleCreateRequest demonstrates the creation of a time-stamp request, sending
// it to the server and parsing the response.
func ExampleCreateRequest() {
	tsq, err := timestamp.CreateRequest(strings.NewReader("ExampleCreateRequestParseResponse"), &timestamp.RequestOptions{
		Hash:         crypto.SHA256,
		Certificates: true,
	})
	if err != nil {
		log.Fatal(err)
	}

	tsr, err := http.Post("https://freetsa.org/tsr", "application/timestamp-query", bytes.NewReader(tsq))
	if err != nil {
		log.Fatal(err)
	}

	if tsr.StatusCode > 200 {
		log.Fatal(tsr.Status)
	}

	resp, err := ioutil.ReadAll(tsr.Body)
	if err != nil {
		log.Fatal(err)
	}

	tsResp, err := timestamp.ParseResponse(resp)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println(tsResp.HashedMessage)
	fmt.Println(tsResp.Policy)
	if len(tsResp.Certificates) > 0 {
		fmt.Println(tsResp.Certificates[0].Subject)
	}

}

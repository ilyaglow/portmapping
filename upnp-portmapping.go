package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/huin/goupnp/dcps/internetgateway1"
	"github.com/huin/goupnp/httpu"
	"github.com/huin/goupnp/soap"
)

const (
	maxWaitSeconds = 5
	methodSearch   = "M-SEARCH"
	searchTarget   = "upnp:rootdevice"
	ssdpDiscover   = "ssdp:discover"
	numSends       = 2
)

// upnpLocation returns a URL address of the UPnP daemon
func upnpLocation(host string, port string) (*url.URL, error) {
	udpcl, err := httpu.NewHTTPUClient()
	if err != nil {
		return nil, err
	}

	resp, err := ssdpRawSearch(udpcl, host+port)
	if err != nil {
		return nil, err
	}

	rawurl := resp.Header.Get("Location")

	loc, err := url.Parse(rawurl)
	if err != nil {
		return nil, err
	}
	log.Printf("UPnP daemon location: %s\n", rawurl)

	if strings.Contains(loc.Host, ":") {
		upnpPort := strings.Split(loc.Host, ":")[1]
		loc.Host = fmt.Sprintf("%s:%s", host, upnpPort)
	} else {
		loc.Host = host
	}

	return loc, nil
}

func ssdpRawSearch(httpu *httpu.HTTPUClient, host string) (*http.Response, error) {
	seenUsns := make(map[string]bool)
	var responses []*http.Response
	req := http.Request{
		Method: methodSearch,
		// TODO: Support both IPv4 and IPv6.
		Host: host,
		URL:  &url.URL{Opaque: "*"},
		Header: http.Header{
			// Putting headers in here avoids them being title-cased.
			// (The UPnP discovery protocol uses case-sensitive headers)
			"HOST": []string{host},
			"MX":   []string{strconv.FormatInt(int64(maxWaitSeconds), 10)},
			"MAN":  []string{ssdpDiscover},
			"ST":   []string{searchTarget},
		},
	}
	allResponses, err := httpu.Do(&req, time.Duration(maxWaitSeconds)*time.Second+100*time.Millisecond, numSends)
	if err != nil {
		return nil, err
	}

	for _, response := range allResponses {
		if response.StatusCode != 200 {
			log.Printf("ssdp: got response status code %q in search response", response.Status)
			continue
		}

		location, err := response.Location()
		if err != nil {
			log.Printf("ssdp: no usable location in search response (discarding): %v", err)
			continue
		}

		usn := response.Header.Get("USN")
		if usn == "" {
			log.Printf("ssdp: empty/missing USN in search response (using location instead): %v", err)
			usn = location.String()
		}
		if _, alreadySeen := seenUsns[usn]; !alreadySeen {
			seenUsns[usn] = true
			responses = append(responses, response)
		}
	}

	if len(responses) == 0 {
		return nil, errors.New("No SSDP response avaiable")
	}

	return responses[0], nil
}

// PortMappingEntry represents a NAT port mapping entry
type PortMappingEntry struct {
	NewRemoteHost             string
	NewExternalPort           string
	NewProtocol               string
	NewInternalPort           string
	NewInternalClient         string
	NewEnabled                string
	NewPortMappingDescription string
	NewLeaseDuration          string
}

type portMappingRequest struct {
	NewPortMappingIndex string
}

func portMappingByIdx(conn *internetgateway1.WANIPConnection1, index uint16) (*PortMappingEntry, error) {
	var (
		si  string
		err error
	)

	if si, err = soap.MarshalUi2(index); err != nil {
		return nil, err
	}

	pmr := &portMappingRequest{si}

	pme := &PortMappingEntry{}
	if err := conn.SOAPClient.PerformAction(internetgateway1.URN_WANIPConnection_1, "GetGenericPortMappingEntry", pmr, pme); err != nil {
		return nil, err
	}

	return pme, nil
}

func main() {
	host := flag.String("host", "", "Host")
	port := flag.String("p", ":1900", "Port")
	flag.Parse()

	loc, err := upnpLocation(*host, *port)
	if err != nil {
		log.Fatal(err)
	}

	ipclients, err := internetgateway1.NewWANIPConnection1ClientsByURL(loc)
	if err != nil {
		log.Fatal(err)
	}

	for _, c := range ipclients {
		dev := &c.ServiceClient.RootDevice.Device
		srv := c.ServiceClient.Service
		log.Println(dev.FriendlyName, " :: ", srv.String())

		for i := 0; i < 50; i++ {
			pme, err := portMappingByIdx(c, uint16(i))
			if err != nil {
				log.Fatal(err)
			}

			log.Println(pme)
		}
	}
}

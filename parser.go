package main

import (
	"encoding/json"
	"io/ioutil"
	"fmt"
	"log"
	//"strings"
	//"io"
	//"io/ioutil"
	//"os"
	"net"
	"bufio"
	"time"
	"sync"
	"flag"
	"github.com/sfreiberg/gotwilio"
)

func p(t interface{}) {
	//fmt.Printf("%T: %v\n", t, t)
}

type Status struct {
	Icao, Call string
	Alt int
	Lat, Long float64
}

type TraceInfo struct {
	lastStatus Status
	compoundStatus Status
	count int
	lastActive time.Time
	notifiedStart bool
	notifiedEnd bool
}

func merge_status(into *Status, from *Status) {
	if len(from.Call) > 0 {
		into.Call = from.Call
	} 
	if from.Alt != 0 {
		into.Alt = from.Alt
	}
	if from.Lat != 0 && from.Long != 0{
		into.Lat = from.Lat
		into.Long = from.Long
	}
}

type TwilioConfig struct {
	Sid, Token, From, NotifyFile string
}

type NotifyConfig struct {
	Code string `json:"code"`
	To []string `json:"to"`
}

type NotifyConfigs struct {
	Configs []NotifyConfig `json:"config"`
}

type NotificationMessage struct { 
	Topic string
	Message string
}

func drain(c chan NotificationMessage, routing map[string][]string) {
	/*
	for {
		<- c
	}*/
	for item := range c {
		toList, ok := routing[item.Topic]
		if ok {
			for _, to := range toList {
				fmt.Printf("[to %v] %v\n", to, item)
			}
		} else {
			fmt.Printf("[no route] %v\n", item)
		}
	}
}

func read_routing_config(filename string) *NotifyConfigs {
	contents, err := ioutil.ReadFile(filename)
	if err != nil {
		log.Fatal("couldn't open notification routing file")
	}

	var config NotifyConfigs
	err = json.Unmarshal(contents, &config)
	if err != nil {
		log.Fatal("couldn't process notification routing file")
	}

	return &config
}

func send_sms(config TwilioConfig, messages chan NotificationMessage) {
	routing := make(map[string][]string)

	if len(config.NotifyFile) > 0 {
		log.Println("Reading notification config")
		configs := read_routing_config(config.NotifyFile)
		for _, config := range configs.Configs {
			routing[config.Code] = config.To
			log.Printf("Route for code %v to %v", config.Code, config.To)
		}
	}

	if len(config.Sid) == 0 || 
		len(config.Token) == 0 ||
		len(config.From) == 0 {
			fmt.Printf("Twilio not set up, not sending SMSes\n")
			drain(messages, routing)
	} else {
		twilio := gotwilio.NewTwilioClient(config.Sid, config.Token)

		for msg := range messages {
			toList, ok := routing[msg.Topic]
			if ok {
				for _, to := range toList {
					twilio.SendSMS(config.From, to, msg.Message, "", "")
					fmt.Printf("sent from %v to %v: [%v]\n", config.From, to, msg.Message)
				}
			} else {
				fmt.Printf("[no route, not sent] [%v]\n", msg.Message)
			}
		}
	}
}

func process_starting_notifications(notifications chan *TraceInfo, messages chan NotificationMessage, airport_idx *IndexRecord) {
	for trace := range notifications {
		nearest, found := NearestAirport(trace.compoundStatus.Lat, trace.compoundStatus.Long, 15, airport_idx)
		if found {
			messages <- NotificationMessage { trace.compoundStatus.Icao, fmt.Sprintf("%v (%v) is flying, seen near %v", trace.compoundStatus.Icao, trace.compoundStatus.Call, nearest) }
		} else {
			messages <- NotificationMessage { trace.compoundStatus.Icao, fmt.Sprintf("%v (%v) is flying", trace.compoundStatus.Icao, trace.compoundStatus.Call) }
		}
		//fmt.Printf("START got notified of trace for %v\n", trace)
		trace.notifiedStart = true
	}
}

func process_ending_notifications(notifications chan *TraceInfo, messages chan NotificationMessage, airport_idx *IndexRecord) {
	for trace := range notifications {
		nearest, found := NearestAirport(trace.compoundStatus.Lat, trace.compoundStatus.Long, 15, airport_idx)
		if found {
			messages <- NotificationMessage { trace.compoundStatus.Icao, fmt.Sprintf("%v (%v) is no longer flying, last seen near %v", trace.compoundStatus.Icao, trace.compoundStatus.Call, nearest) }
		} else {
			messages <- NotificationMessage { trace.compoundStatus.Icao, fmt.Sprintf("%v (%v) is no longer flying", trace.compoundStatus.Icao, trace.compoundStatus.Call) }
		}
		//fmt.Printf("END got notified of trace for %v\n", trace)
		trace.notifiedEnd = true
	}
}

func should_notify(trace *TraceInfo) bool {
	//is the airplane flying and is there a location set?
	ok := trace.compoundStatus.Lat != 0 && trace.count > 3 && !trace.notifiedStart
	if !ok {
		//maybe it's flying but there is no location, but we should notify anyway
		ok = trace.count > 10 && !trace.notifiedStart
	}
	return ok
}

func should_clean_trace(trace *TraceInfo) bool {
	return time.Now().Sub(trace.lastActive) > time.Duration(10 * time.Minute)
}


func cleanup_active(actives *ActiveTracker, notifications chan *TraceInfo) {
	var to_clean []string
	fmt.Printf("%v records in active, ", len(actives.active))
	for icao, trace := range actives.active {
		if should_clean_trace(trace) {
			to_clean = append(to_clean, icao)
		}
	}
	fmt.Printf("will clean %v\n", len(to_clean))
	if len(to_clean) > 0 {
		actives.Lock()
		for _, icao := range to_clean {
			trace := actives.active[icao]
			if trace.notifiedStart {
				notifications <- trace
			}

			delete(actives.active, icao)
		}
		actives.Unlock()
	}
}

type ActiveTracker struct {
	sync.RWMutex
	active map[string]*TraceInfo
}

func NewActiveTracker() *ActiveTracker {
	var at ActiveTracker 
	at.active = make(map[string]*TraceInfo)
	return &at
}

func aggregate_traces(actives *ActiveTracker, statuses chan *Status, starting_notifications chan *TraceInfo) {

	for  status := range statuses {
		actives.RLock()
		trace_record, ok := actives.active[status.Icao]
		actives.RUnlock()
		if ok == false {
			trace_record = &TraceInfo { *status, *status, 1, time.Now(), false, false }
			actives.Lock()
			actives.active[status.Icao] = trace_record
			actives.Unlock()
		} else {
			trace_record.lastStatus = *status
			merge_status(&trace_record.compoundStatus, status)
			trace_record.count++
			trace_record.lastActive = time.Now()
		}

		if should_notify(trace_record) {
			starting_notifications <- trace_record
		}
	}
}

func run_cleanup(actives *ActiveTracker, ending_notifications chan *TraceInfo) {
	lastCleanup := time.Now()
	for {
		if time.Now().Sub(lastCleanup) > time.Duration(1 * time.Minute) {
			cleanup_active(actives, ending_notifications)
			lastCleanup = time.Now()
		}

		time.Sleep(time.Duration(2 * time.Second))
	}
}

func should_accept_status(status *Status) bool {
	//always capture N3587X
	if status.Icao == "A405E8"  {
		return true
	}	

	//always capture N21643
	if status.Icao == "A1D2CC" {
		return true
	}

	//always capture N73614
	if status.Icao == "A9E3ED" {
		return true
	}
	
	/*
	if status.Alt > 0 &&
		len(status.Call) > 0 &&
		strings.HasPrefix(status.Call, "N") &&
		status.Lat != 0 &&
		status.Long != 0 {
		return true
	}

	if strings.HasPrefix(status.Icao, "A7") {
		return true
	}	
	*/

	return false
}

func run_connection(statuses chan *Status, airplane_idx *AirplaneIndex) {

	f, err := net.Dial("tcp", "pub-vrs.adsbexchange.com:32001")
    if err != nil {
        log.Printf("dial error:", err)
        return
    } else { 
    	log.Println("Reconnected")
    }
    defer f.Close()

	dec := json.NewDecoder(bufio.NewReader(f))

	for {
		// read open bracket
		t, err := dec.Token()
		if err != nil {
			log.Println(err)
			return
		}
		p(t)

		t, err = dec.Token()
		if err != nil || t != "acList" {
			log.Println("no acList")
			return
		}
		p(t)
		// read array start
		t, err = dec.Token()
		if err != nil {
			log.Println(err)
			return
		}
		p(t)

		//fmt.Printf("%T: %v\n", t, t)

		// while the array contains values
		count := 0
		for dec.More() {
			count = count + 1
			var m Status
			// decode an array value (Message)
			err := dec.Decode(&m)
			if err != nil {
				log.Println(err)
				return
			}

			if should_accept_status(&m) {
				if (len(m.Call) == 0) {
					m.Call = FindAirplaneByHex(m.Icao, airplane_idx)
				}

				fmt.Printf("Status: %v\n", m)
				statuses <- &m
			}
			
		}

		//fmt.Printf("read %v more loops\n", count)

		// read closing bracket
		t, err = dec.Token()
		if err != nil {
			log.Println(err)
			return
		}
		p(t)

		//read end of acList
		t, err = dec.Token()
		if err != nil {
			log.Println(err)
			return
		}
		p(t)
	}

}

func main() {

	var twilioConfig TwilioConfig
	flag.StringVar(&twilioConfig.Sid, "Sid", "", "Account SID")
	flag.StringVar(&twilioConfig.Token, "Token", "", "Account Token")
	flag.StringVar(&twilioConfig.From, "From", "", "From number (+123456...)")
	flag.StringVar(&twilioConfig.NotifyFile, "NotifyFile", "", "Notification file")
	flag.Parse()

    statuses := make(chan *Status)
    starting_notifications := make(chan *TraceInfo)
    ending_notifications := make(chan *TraceInfo)
    messages := make(chan NotificationMessage)

    actives := NewActiveTracker()

	airport_idx := PrepareIndex()
	airplane_idx := NewAirplaneIndex()

    go aggregate_traces(actives, statuses, starting_notifications)
    go process_starting_notifications(starting_notifications, messages, airport_idx)
    go process_ending_notifications(ending_notifications, messages, airport_idx)
    go run_cleanup(actives, ending_notifications)
    go send_sms(twilioConfig, messages)

    last_connection := time.Now()
    reconnections := 0

    for {
    	last_connection = time.Now()

    	run_connection(statuses, airplane_idx)

    	if time.Now().Sub(last_connection) < time.Duration(1 * time.Minute) {
    		reconnections++
    	} else {
    		reconnections = 0
    	}

    	if reconnections > 3 {
    		log.Fatal("too many reconnections without success!")
    	}

    }

}
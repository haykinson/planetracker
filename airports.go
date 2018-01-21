package main

import (
	"github.com/mmcloughlin/openflights"
	"github.com/hailocab/go-geoindex"
	//"fmt"
)


type LocalAirport struct {
	ICAO string
	Latitude float64
	Longitude float64
}

func (d *LocalAirport) Lat() float64 { return d.Latitude }
func (d *LocalAirport) Lon() float64 { return d.Longitude }
func (d *LocalAirport) Id() string { return d.ICAO }

func build_lookup_index() *map[string]openflights.Airport{
	result := make(map[string]openflights.Airport)
	for _, record := range openflights.Airports {
		result[record.ICAO] = record
	}

	return &result
}


func build_geo_index() *geoindex.PointsIndex {
	index := geoindex.NewPointsIndex(geoindex.Km(0.5))
	for _, record := range openflights.Airports {
		index.Add(&LocalAirport { record.ICAO, record.Latitude, record.Longitude })
	}

	return index
}

type IndexRecord struct {
	index *geoindex.PointsIndex
}

func PrepareIndex() *IndexRecord {
	return &IndexRecord { build_geo_index() }
}

func NearestAirport(lat float64, lon float64, radiusKM float64, index *IndexRecord) (string, bool) {
	nrst := index.index.KNearest(&geoindex.GeoPoint{"", lat, lon}, 1, geoindex.Km(radiusKM), func(p geoindex.Point) bool {
        return true
    })

	if len(nrst) > 0 {
		return nrst[0].Id(), true 
	} else {
		return "", false
	}
}

/*
func main() {

	idx := PrepareIndex()

	fmt.Printf("Nearest to 34.210602 -119.078274:\n")

	if icao, found := NearestAirport(34.210602, -119.078274, 15, idx); found {
		fmt.Println(icao)
	} else {
		fmt.Println("not found")
	}

}
*/
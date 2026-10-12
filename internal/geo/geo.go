package geo

import "math"

const earthRadius = 6371000 // meters

// Point is a location specified in degrees
type Point struct {
	Lat float64
	Lon float64
}

// Distance returns the great-circle distance between points a and b in meters (using the haversine formula)
func Distance(a, b Point) float64 {
	lat1 := a.Lat * math.Pi / 180
	lat2 := b.Lat * math.Pi / 180
	dLat := (b.Lat - a.Lat) * math.Pi / 180
	dLon := (b.Lon - a.Lon) * math.Pi / 180

	h := math.Sin(dLat/2)*math.Sin(dLat/2) + math.Cos(lat1)*math.Cos(lat2)*math.Sin(dLon/2)*math.Sin(dLon/2)

	return 2 * earthRadius * math.Asin(math.Sqrt(h))
}

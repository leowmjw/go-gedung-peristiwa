package gtfs

const (
	minLat = 1.0
	maxLat = 7.0
	minLng = 100.0
	maxLng = 119.0
)

// ValidMalaysiaBounds reports whether lat/lng fall within Malaysia's bounding box.
func ValidMalaysiaBounds(lat, lng float64) bool {
	return lat >= minLat && lat <= maxLat && lng >= minLng && lng <= maxLng
}

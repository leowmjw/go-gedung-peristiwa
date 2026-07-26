package gtfs

import "fmt"

const DefaultRegionID = "klang-valley"

// MapRegion is a top-level authoritative map bucket.
type MapRegion struct {
	ID      string
	Label   string
	Center  [2]float64 // lat, lng
	Zoom    int
	Agencies []string
}

var mapRegions = []MapRegion{
	{
		ID: "klang-valley", Label: "Klang Valley", Center: [2]float64{3.139, 101.687}, Zoom: 12,
		Agencies: []string{"prasarana-rapid-bus-kl", "prasarana-rapid-bus-mrtfeeder"},
	},
	{
		ID: "national", Label: "National", Center: [2]float64{4.2, 102.0}, Zoom: 7,
		Agencies: []string{"ktmb"},
	},
	{
		ID: "penang", Label: "Penang", Center: [2]float64{5.41, 100.33}, Zoom: 11,
		Agencies: []string{"prasarana-rapid-bus-penang"},
	},
	{
		ID: "east-coast", Label: "East Coast", Center: [2]float64{4.95, 103.15}, Zoom: 8,
		Agencies: []string{"prasarana-rapid-bus-kuantan", "mybas-kota-bharu", "mybas-kuala-terengganu"},
	},
	{
		ID: "johor", Label: "Johor", Center: [2]float64{1.49, 103.74}, Zoom: 11,
		Agencies: []string{"mybas-johor"},
	},
	{
		ID: "sarawak", Label: "Sarawak", Center: [2]float64{1.55, 110.34}, Zoom: 11,
		Agencies: []string{"mybas-kuching"},
	},
	{
		ID: "northern", Label: "Northern", Center: [2]float64{5.55, 100.75}, Zoom: 8,
		Agencies: []string{"mybas-kangar", "mybas-alor-setar", "mybas-ipoh"},
	},
	{
		ID: "central", Label: "Central", Center: [2]float64{2.55, 102.15}, Zoom: 9,
		Agencies: []string{"mybas-seremban-a", "mybas-seremban-b", "mybas-melaka"},
	},
}

var agencyToRegion map[string]MapRegion

func init() {
	agencyToRegion = make(map[string]MapRegion, 15)
	for _, r := range mapRegions {
		for _, a := range r.Agencies {
			agencyToRegion[a] = r
		}
	}
}

// DefaultRegion returns the default map region (Klang Valley).
func DefaultRegion() MapRegion {
	return mapRegions[0]
}

// AllRegions returns all top-level map regions.
func AllRegions() []MapRegion {
	out := make([]MapRegion, len(mapRegions))
	copy(out, mapRegions)
	return out
}

// RegionByID returns a region by id or an error.
func RegionByID(id string) (MapRegion, error) {
	for _, r := range mapRegions {
		if r.ID == id {
			return r, nil
		}
	}
	return MapRegion{}, fmt.Errorf("unknown region %q", id)
}

// AgenciesForRegion returns agency ids for a region bucket.
func AgenciesForRegion(id string) ([]string, error) {
	r, err := RegionByID(id)
	if err != nil {
		return nil, err
	}
	out := make([]string, len(r.Agencies))
	copy(out, r.Agencies)
	return out, nil
}

// RegionForAgency returns the map region for an agency id.
func RegionForAgency(agency string) (MapRegion, bool) {
	r, ok := agencyToRegion[agency]
	return r, ok
}

// FeedsForRegions returns the union of feeds for multiple region buckets.
func FeedsForRegions(ids []string) ([]Feed, error) {
	seen := make(map[string]struct{})
	var out []Feed
	for _, id := range ids {
		feeds, err := FeedsForRegion(id)
		if err != nil {
			return nil, err
		}
		for _, f := range feeds {
			if _, ok := seen[f.Agency]; ok {
				continue
			}
			seen[f.Agency] = struct{}{}
			out = append(out, f)
		}
	}
	return out, nil
}

// FeedsForRegion returns feeds belonging to a region bucket.
func FeedsForRegion(id string) ([]Feed, error) {
	r, err := RegionByID(id)
	if err != nil {
		return nil, err
	}
	agencySet := make(map[string]struct{}, len(r.Agencies))
	for _, a := range r.Agencies {
		agencySet[a] = struct{}{}
	}
	var out []Feed
	for _, f := range AllFeeds() {
		if _, ok := agencySet[f.Agency]; ok {
			out = append(out, f)
		}
	}
	return out, nil
}

// AgencySet returns a set of agency ids.
func AgencySet(ids []string) map[string]struct{} {
	set := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		set[id] = struct{}{}
	}
	return set
}

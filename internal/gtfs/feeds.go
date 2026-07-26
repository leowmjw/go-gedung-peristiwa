package gtfs

const baseURL = "https://api.data.gov.my/gtfs-realtime/vehicle-position/"

// Feed describes one Malaysian GTFS-R vehicle position endpoint.
type Feed struct {
	Agency   string // IsleDB prefix / filter id
	URL      string // full API URL
	Category string // Prasarana category param, if any
	Type     string // "rail" or "bus"
	Region   string // display region name
	Group    string // "ktmb", "prasarana", or "mybas"
	BucketID string // top-level map region id (see regions.go)
}

// KLFeeds returns Prasarana RapidKL bus feeds for the Klang Valley demo.
func KLFeeds() []Feed {
	feeds, _ := FeedsForRegion(DefaultRegionID)
	return feeds
}

// AllFeeds returns all 15 Malaysian GTFS-R vehicle position feeds.
func AllFeeds() []Feed {
	return []Feed{
		{Agency: "ktmb", URL: baseURL + "ktmb", Type: "rail", Region: "National", Group: "ktmb", BucketID: "national"},
		{Agency: "prasarana-rapid-bus-kl", URL: baseURL + "prasarana?category=rapid-bus-kl", Category: "rapid-bus-kl", Type: "bus", Region: "KL", Group: "prasarana", BucketID: "klang-valley"},
		{Agency: "prasarana-rapid-bus-mrtfeeder", URL: baseURL + "prasarana?category=rapid-bus-mrtfeeder", Category: "rapid-bus-mrtfeeder", Type: "bus", Region: "KL MRT Feeder", Group: "prasarana", BucketID: "klang-valley"},
		{Agency: "prasarana-rapid-bus-kuantan", URL: baseURL + "prasarana?category=rapid-bus-kuantan", Category: "rapid-bus-kuantan", Type: "bus", Region: "Kuantan", Group: "prasarana", BucketID: "east-coast"},
		{Agency: "prasarana-rapid-bus-penang", URL: baseURL + "prasarana?category=rapid-bus-penang", Category: "rapid-bus-penang", Type: "bus", Region: "Penang", Group: "prasarana", BucketID: "penang"},
		{Agency: "mybas-kangar", URL: baseURL + "mybas-kangar", Type: "bus", Region: "Perlis", Group: "mybas", BucketID: "northern"},
		{Agency: "mybas-alor-setar", URL: baseURL + "mybas-alor-setar", Type: "bus", Region: "Kedah", Group: "mybas", BucketID: "northern"},
		{Agency: "mybas-kota-bharu", URL: baseURL + "mybas-kota-bharu", Type: "bus", Region: "Kelantan", Group: "mybas", BucketID: "east-coast"},
		{Agency: "mybas-kuala-terengganu", URL: baseURL + "mybas-kuala-terengganu", Type: "bus", Region: "Terengganu", Group: "mybas", BucketID: "east-coast"},
		{Agency: "mybas-ipoh", URL: baseURL + "mybas-ipoh", Type: "bus", Region: "Perak", Group: "mybas", BucketID: "northern"},
		{Agency: "mybas-seremban-a", URL: baseURL + "mybas-seremban-a", Type: "bus", Region: "N. Sembilan A", Group: "mybas", BucketID: "central"},
		{Agency: "mybas-seremban-b", URL: baseURL + "mybas-seremban-b", Type: "bus", Region: "N. Sembilan B", Group: "mybas", BucketID: "central"},
		{Agency: "mybas-melaka", URL: baseURL + "mybas-melaka", Type: "bus", Region: "Melaka", Group: "mybas", BucketID: "central"},
		{Agency: "mybas-johor", URL: baseURL + "mybas-johor", Type: "bus", Region: "Johor", Group: "mybas", BucketID: "johor"},
		{Agency: "mybas-kuching", URL: baseURL + "mybas-kuching", Type: "bus", Region: "Sarawak", Group: "mybas", BucketID: "sarawak"},
	}
}

// AgencyIDs returns the agency prefix for each feed.
func AgencyIDs(feeds []Feed) []string {
	ids := make([]string, len(feeds))
	for i, f := range feeds {
		ids[i] = f.Agency
	}
	return ids
}

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
}

// KLFeeds returns Prasarana RapidKL bus feeds for the Klang Valley demo.
func KLFeeds() []Feed {
	return []Feed{
		{Agency: "prasarana-rapid-bus-kl", URL: baseURL + "prasarana?category=rapid-bus-kl", Category: "rapid-bus-kl", Type: "bus", Region: "RapidKL Bus", Group: "prasarana"},
		{Agency: "prasarana-rapid-bus-mrtfeeder", URL: baseURL + "prasarana?category=rapid-bus-mrtfeeder", Category: "rapid-bus-mrtfeeder", Type: "bus", Region: "MRT Feeder", Group: "prasarana"},
	}
}

// AllFeeds returns all 15 Malaysian GTFS-R vehicle position feeds.
func AllFeeds() []Feed {
	return []Feed{
		{Agency: "ktmb", URL: baseURL + "ktmb", Type: "rail", Region: "National", Group: "ktmb"},
		{Agency: "prasarana-rapid-bus-kl", URL: baseURL + "prasarana?category=rapid-bus-kl", Category: "rapid-bus-kl", Type: "bus", Region: "KL", Group: "prasarana"},
		{Agency: "prasarana-rapid-bus-mrtfeeder", URL: baseURL + "prasarana?category=rapid-bus-mrtfeeder", Category: "rapid-bus-mrtfeeder", Type: "bus", Region: "KL MRT Feeder", Group: "prasarana"},
		{Agency: "prasarana-rapid-bus-kuantan", URL: baseURL + "prasarana?category=rapid-bus-kuantan", Category: "rapid-bus-kuantan", Type: "bus", Region: "Kuantan", Group: "prasarana"},
		{Agency: "prasarana-rapid-bus-penang", URL: baseURL + "prasarana?category=rapid-bus-penang", Category: "rapid-bus-penang", Type: "bus", Region: "Penang", Group: "prasarana"},
		{Agency: "mybas-kangar", URL: baseURL + "mybas-kangar", Type: "bus", Region: "Perlis", Group: "mybas"},
		{Agency: "mybas-alor-setar", URL: baseURL + "mybas-alor-setar", Type: "bus", Region: "Kedah", Group: "mybas"},
		{Agency: "mybas-kota-bharu", URL: baseURL + "mybas-kota-bharu", Type: "bus", Region: "Kelantan", Group: "mybas"},
		{Agency: "mybas-kuala-terengganu", URL: baseURL + "mybas-kuala-terengganu", Type: "bus", Region: "Terengganu", Group: "mybas"},
		{Agency: "mybas-ipoh", URL: baseURL + "mybas-ipoh", Type: "bus", Region: "Perak", Group: "mybas"},
		{Agency: "mybas-seremban-a", URL: baseURL + "mybas-seremban-a", Type: "bus", Region: "N. Sembilan A", Group: "mybas"},
		{Agency: "mybas-seremban-b", URL: baseURL + "mybas-seremban-b", Type: "bus", Region: "N. Sembilan B", Group: "mybas"},
		{Agency: "mybas-melaka", URL: baseURL + "mybas-melaka", Type: "bus", Region: "Melaka", Group: "mybas"},
		{Agency: "mybas-johor", URL: baseURL + "mybas-johor", Type: "bus", Region: "Johor", Group: "mybas"},
		{Agency: "mybas-kuching", URL: baseURL + "mybas-kuching", Type: "bus", Region: "Sarawak", Group: "mybas"},
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

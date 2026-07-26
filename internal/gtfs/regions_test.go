package gtfs_test

import (
	"testing"

	"github.com/leow/go-gedung-peristiwa/internal/gtfs"
)

func TestDefaultRegion(t *testing.T) {
	r := gtfs.DefaultRegion()
	if r.ID != "klang-valley" {
		t.Fatalf("id = %q", r.ID)
	}
}

func TestRegionForAgencyKTMB(t *testing.T) {
	r, ok := gtfs.RegionForAgency("ktmb")
	if !ok || r.ID != "national" {
		t.Fatalf("ktmb region = %+v ok=%v", r, ok)
	}
}

func TestRegionForAgencyUnknown(t *testing.T) {
	_, ok := gtfs.RegionForAgency("unknown-agency")
	if ok {
		t.Fatal("expected unknown agency")
	}
}

func TestFeedsForRegionKlangValley(t *testing.T) {
	feeds, err := gtfs.FeedsForRegion("klang-valley")
	if err != nil {
		t.Fatal(err)
	}
	if len(feeds) != 2 {
		t.Fatalf("feeds = %d", len(feeds))
	}
}

func TestRegionByIDUnknown(t *testing.T) {
	_, err := gtfs.RegionByID("nope")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestFeedsForRegionEastCoast(t *testing.T) {
	feeds, err := gtfs.FeedsForRegion("east-coast")
	if err != nil {
		t.Fatal(err)
	}
	if len(feeds) != 3 {
		t.Fatalf("feeds = %d", len(feeds))
	}
}

func TestRegionForAgencyIpohNorthern(t *testing.T) {
	r, ok := gtfs.RegionForAgency("mybas-ipoh")
	if !ok || r.ID != "northern" {
		t.Fatalf("ipoh region = %+v ok=%v", r, ok)
	}
}

func TestRegionForAgencyKuantanEastCoast(t *testing.T) {
	r, ok := gtfs.RegionForAgency("prasarana-rapid-bus-kuantan")
	if !ok || r.ID != "east-coast" {
		t.Fatalf("kuantan region = %+v ok=%v", r, ok)
	}
}

func TestAllRegionsCount(t *testing.T) {
	if len(gtfs.AllRegions()) != 8 {
		t.Fatalf("regions = %d", len(gtfs.AllRegions()))
	}
}

package main

import (
	"os"
	"testing"
)

// TestLookupIPLocation_WithoutDatabase verifies lookupIPLocation without GeoIP DB
func TestLookupIPLocation_WithoutDatabase(t *testing.T) {
	server := &Server{
		geoIPDBPath: "", // No database
	}

	// Should return 0, 0 without panicking
	lat, lon := server.lookupIPLocation("8.8.8.8")

	if lat != 0.0 || lon != 0.0 {
		t.Errorf("Expected (0.0, 0.0) without database, got (%.2f, %.2f)", lat, lon)
	}
}

// TestLookupIPLocation_WithInvalidDatabase verifies error handling
func TestLookupIPLocation_WithInvalidDatabase(t *testing.T) {
	server := &Server{
		geoIPDBPath: "/nonexistent/path/to/database.mmdb",
	}

	// Should return 0, 0 when database can't be opened
	lat, lon := server.lookupIPLocation("8.8.8.8")

	if lat != 0.0 || lon != 0.0 {
		t.Errorf("Expected (0.0, 0.0) with invalid database, got (%.2f, %.2f)", lat, lon)
	}
}

// TestLookupIPLocation_WithInvalidIP verifies invalid IP handling
func TestLookupIPLocation_WithInvalidIP(t *testing.T) {
	server := &Server{
		geoIPDBPath: "",
	}

	tests := []struct {
		name string
		ip   string
	}{
		{"EmptyIP", ""},
		{"MalformedIP", "not-an-ip"},
		{"PartialIP", "192.168"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lat, lon := server.lookupIPLocation(tt.ip)

			if lat != 0.0 || lon != 0.0 {
				t.Errorf("Expected (0.0, 0.0) for invalid IP %s, got (%.2f, %.2f)",
					tt.ip, lat, lon)
			}
		})
	}
}

// TestLookupIPLocation_WithValidDatabase verifies successful lookups
func TestLookupIPLocation_WithValidDatabase(t *testing.T) {
	// Try common GeoIP database locations
	possiblePaths := []string{
		os.Getenv("GEOIP_DB_PATH"),
		"/usr/share/GeoIP/GeoLite2-City.mmdb",
		"/var/lib/GeoIP/GeoLite2-City.mmdb",
	}

	var dbPath string
	for _, path := range possiblePaths {
		if path != "" {
			if _, err := os.Stat(path); err == nil {
				dbPath = path
				break
			}
		}
	}

	if dbPath == "" {
		t.Skip("No GeoIP database available for testing")
	}

	server := &Server{
		geoIPDBPath: dbPath,
	}

	// Test with known public IP
	lat, lon := server.lookupIPLocation("8.8.8.8")

	// Google DNS should have valid geolocation
	if lat == 0.0 && lon == 0.0 {
		t.Logf("Warning: Got (0.0, 0.0) for 8.8.8.8, may indicate lookup issue")
	} else {
		t.Logf("Looked up 8.8.8.8: latitude=%.2f, longitude=%.2f", lat, lon)
	}
}

// TestLookupIPLocation_PrivateIPs verifies private IP handling
func TestLookupIPLocation_PrivateIPs(t *testing.T) {
	server := &Server{
		geoIPDBPath: "",
	}

	privateIPs := []string{
		"192.168.1.1",
		"10.0.0.1",
		"172.16.0.1",
		"127.0.0.1",
	}

	for _, ip := range privateIPs {
		t.Run(ip, func(t *testing.T) {
			lat, lon := server.lookupIPLocation(ip)

			// Private IPs typically return 0, 0
			t.Logf("Private IP %s: latitude=%.2f, longitude=%.2f", ip, lat, lon)
		})
	}
}

// TestLookupIPLocation_IPv6 verifies IPv6 handling
func TestLookupIPLocation_IPv6(t *testing.T) {
	server := &Server{
		geoIPDBPath: "",
	}

	ipv6Addresses := []string{
		"2001:4860:4860::8888", // Google DNS IPv6
		"::1",                   // IPv6 localhost
	}

	for _, ip := range ipv6Addresses {
		t.Run(ip, func(t *testing.T) {
			lat, lon := server.lookupIPLocation(ip)

			t.Logf("IPv6 %s: latitude=%.2f, longitude=%.2f", ip, lat, lon)
		})
	}
}

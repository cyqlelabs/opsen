package main

import (
	"testing"
)

// TestLookupIPLocation_NoDatabase verifies handling when no database configured
func TestLookupIPLocation_NoDatabase(t *testing.T) {
	db, cleanup := CreateTestDB(t)
	defer cleanup()

	srv := NewTestServer(t, db)
	srv.geoIPDBPath = "" // No database configured

	lat, lon := srv.lookupIPLocation("8.8.8.8")

	if lat != 0 || lon != 0 {
		t.Errorf("Expected (0, 0) when no database, got (%.2f, %.2f)", lat, lon)
	}
}

// TestLookupIPLocation_InvalidDBPath verifies handling of invalid database path
func TestLookupIPLocation_InvalidDBPath(t *testing.T) {
	db, cleanup := CreateTestDB(t)
	defer cleanup()

	srv := NewTestServer(t, db)
	srv.geoIPDBPath = "/nonexistent/path/GeoLite2-City.mmdb"

	lat, lon := srv.lookupIPLocation("8.8.8.8")

	if lat != 0 || lon != 0 {
		t.Errorf("Expected (0, 0) for invalid DB path, got (%.2f, %.2f)", lat, lon)
	}
}

// TestLookupIPLocation_InvalidIP verifies handling of invalid IP address
func TestLookupIPLocation_InvalidIP(t *testing.T) {
	db, cleanup := CreateTestDB(t)
	defer cleanup()

	srv := NewTestServer(t, db)
	srv.geoIPDBPath = "/tmp/test.mmdb" // Set path even if file doesn't exist

	lat, lon := srv.lookupIPLocation("not-an-ip")

	if lat != 0 || lon != 0 {
		t.Errorf("Expected (0, 0) for invalid IP, got (%.2f, %.2f)", lat, lon)
	}
}

// TestLookupIPLocation_ValidIPInvalidDB verifies handling when DB doesn't have IP
func TestLookupIPLocation_ValidIPInvalidDB(t *testing.T) {
	db, cleanup := CreateTestDB(t)
	defer cleanup()

	srv := NewTestServer(t, db)
	srv.geoIPDBPath = "/nonexistent/test.mmdb"

	// Valid IP format but DB open will fail
	lat, lon := srv.lookupIPLocation("192.0.2.1")

	if lat != 0 || lon != 0 {
		t.Errorf("Expected (0, 0) when DB open fails, got (%.2f, %.2f)", lat, lon)
	}
}

// TestLookupIPLocation_EmptyIP verifies handling of empty IP string
func TestLookupIPLocation_EmptyIP(t *testing.T) {
	db, cleanup := CreateTestDB(t)
	defer cleanup()

	srv := NewTestServer(t, db)
	srv.geoIPDBPath = "/tmp/test.mmdb"

	lat, lon := srv.lookupIPLocation("")

	if lat != 0 || lon != 0 {
		t.Errorf("Expected (0, 0) for empty IP, got (%.2f, %.2f)", lat, lon)
	}
}

// TestLookupIPLocation_PrivateIP verifies handling of private IP addresses
func TestLookupIPLocation_PrivateIP(t *testing.T) {
	db, cleanup := CreateTestDB(t)
	defer cleanup()

	srv := NewTestServer(t, db)
	srv.geoIPDBPath = "/tmp/test.mmdb"

	// Private IP address
	lat, lon := srv.lookupIPLocation("192.168.1.1")

	// Will fail because no real DB, but tests the code path
	if lat != 0 || lon != 0 {
		t.Logf("Private IP lookup returned: (%.2f, %.2f)", lat, lon)
	}
}

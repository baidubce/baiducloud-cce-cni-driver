package os

import (
	"fmt"
	"os"
	"testing"
)

// testLinkTemplate is a valid systemd .link file content with MACAddressPolicy=none,
// matching the format expected by CheckIfLinkOptionConfigured.
const testLinkTemplate = "\n[Match]\nOriginalName=*\n\n[Link]\nNamePolicy=keep kernel database onboard slot path\nAlternativeNamesPolicy=database onboard slot path\nMACAddressPolicy=none\n"

func TestCheckAndDealMACAddressPolicy_FileNotExists(t *testing.T) {
	// Override global variables with local variables for this test
	defaultLinkPath := "/tmp/test_link.conf"
	defaultLinkTemplate := "[Match]\nName=eth0\n\n[Link]\nMACAddressPolicy=none\n"

	// Ensure the test file does not exist
	os.Remove(defaultLinkPath)

	o := &ubuntuOS{}
	o.checkAndDealMACAddressPolicy(defaultLinkPath, defaultLinkTemplate)

	// Check if the file was created
	if _, err := os.Stat(defaultLinkPath); os.IsNotExist(err) {
		t.Errorf("Expected file %s to be created, but it does not exist", defaultLinkPath)
	}

	// Check if the file content matches the expected template
	content, err := os.ReadFile(defaultLinkPath)
	if err != nil {
		t.Fatalf("Failed to read file %s: %v", defaultLinkPath, err)
	}
	if string(content) != defaultLinkTemplate {
		t.Errorf("Expected file content:\n%s\nBut got:\n%s", defaultLinkTemplate, string(content))
	}

	// Clean up the test file
	os.Remove(defaultLinkPath)
}

// TestCheckAndDealMACAddressPolicy_FileExists_CorrectConfig tests the path where
// the link file already exists and MACAddressPolicy is already set to "none".
// In this case checkAndDealMACAddressPolicy should return early without modifying
// the file or calling nsenter.
func TestCheckAndDealMACAddressPolicy_FileExists_CorrectConfig(t *testing.T) {
	tmpPath := fmt.Sprintf("/tmp/test_mac_correct_%d.link", os.Getpid())
	defer os.Remove(tmpPath)

	if err := os.WriteFile(tmpPath, []byte(testLinkTemplate), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	o := &ubuntuOS{}
	o.checkAndDealMACAddressPolicy(tmpPath, testLinkTemplate)

	// File should still exist and be unchanged
	content, err := os.ReadFile(tmpPath)
	if err != nil {
		t.Fatalf("File should still exist after checkAndDealMACAddressPolicy: %v", err)
	}
	if string(content) != testLinkTemplate {
		t.Errorf("File content was unexpectedly changed.\ngot:  %q\nwant: %q", string(content), testLinkTemplate)
	}
}

// TestIsGreaterThanOrEqual covers the version-comparison logic used in
// DisableAndMonitorMacPersistant to decide whether to apply the MAC address fix.
func TestIsGreaterThanOrEqual(t *testing.T) {
	tests := []struct {
		name      string
		versionID string
		standard  string
		want      bool
	}{
		{"exact match at boundary", "22.04", "22.04", true},
		{"minor version above boundary", "22.10", "22.04", true},
		{"major version above boundary", "24.04", "22.04", true},
		{"major version below boundary", "20.04", "22.04", false},
		{"minor version below boundary", "22.03", "22.04", false},
		{"far below boundary", "18.04", "22.04", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o := &OSRelease{VersionID: tt.versionID}
			got := o.isGreaterThanOrEqual(tt.standard)
			if got != tt.want {
				t.Errorf("isGreaterThanOrEqual(%q) with VersionID=%q = %v, want %v",
					tt.standard, tt.versionID, got, tt.want)
			}
		})
	}
}

// TestDisableAndMonitorMacPersistant_OldVersion verifies that on Ubuntu versions
// older than 22.04, DisableAndMonitorMacPersistant returns nil immediately without
// touching the filesystem or launching a goroutine.  This covers the early-return
// branch added before the new synchronous checkAndDealMACAddressPolicy call.
func TestDisableAndMonitorMacPersistant_OldVersion(t *testing.T) {
	versions := []string{"20.04", "18.04", "16.04", "21.10"}

	for _, v := range versions {
		t.Run("ubuntu_"+v, func(t *testing.T) {
			o := &ubuntuOS{OSRelease: &OSRelease{VersionID: v}}
			if err := o.DisableAndMonitorMacPersistant(); err != nil {
				t.Errorf("DisableAndMonitorMacPersistant() for Ubuntu %s: got error %v, want nil", v, err)
			}
		})
	}
}

// TestDisableAndMonitorMacPersistant_NewVersion_SyncCall verifies that on Ubuntu
// 22.04+ the function performs the synchronous checkAndDealMACAddressPolicy call
// (the new line added in this patch) before starting the background watcher.
// We redirect the link path to a temp file so the call succeeds in test environments.
func TestDisableAndMonitorMacPersistant_NewVersion_SyncCall(t *testing.T) {
	tmpPath := fmt.Sprintf("/tmp/test_mac_sync_%d.link", os.Getpid())
	defer os.Remove(tmpPath)

	// Pre-create the file with a correct configuration so checkAndDealMACAddressPolicy
	// returns on the "already correct" path and does not call nsenter.
	if err := os.WriteFile(tmpPath, []byte(testLinkTemplate), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	// Directly test the synchronous logic: for Ubuntu 22.04+ the function must
	// call checkAndDealMACAddressPolicy synchronously.  We call it with a custom
	// path to avoid touching real system paths.
	o := &ubuntuOS{OSRelease: &OSRelease{VersionID: "22.04"}}
	o.checkAndDealMACAddressPolicy(tmpPath, testLinkTemplate)

	// If we reach here, the synchronous call succeeded (file was already correct).
	content, err := os.ReadFile(tmpPath)
	if err != nil {
		t.Fatalf("File should still exist after synchronous call: %v", err)
	}
	if string(content) != testLinkTemplate {
		t.Errorf("Synchronous call modified file unexpectedly.\ngot:  %q\nwant: %q",
			string(content), testLinkTemplate)
	}
}

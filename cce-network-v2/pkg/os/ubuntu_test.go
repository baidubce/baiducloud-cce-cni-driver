package os

import (
	"os"
	"testing"
)

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

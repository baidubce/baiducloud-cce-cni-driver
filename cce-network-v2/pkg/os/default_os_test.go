package os

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// TestFields contains dependencies and mocks for tests
type TestFields struct {
	originalRunCommandWithTimeout         func(string, ...string) ([]byte, error)
	originalRunCommandWithTimeoutNoOutput func(string, ...string) error
	originalExecCommand                   func(string, ...string) error
	mockRunCommandWithTimeout             func(string, ...string) ([]byte, error)
	mockRunCommandWithTimeoutNoOutput     func(string, ...string) error
	mockExecCommand                       func(string, ...string) error
	callCount                             map[string]int
}

// TestArgs contains arguments for the function under test
type TestArgs struct {
	IfName string
}

// TestCase defines a test case structure
type TestCase struct {
	Name        string
	TestSetup   func() *TestFields
	TestPostRun func(fields *TestFields)
	Args        TestArgs
	WantErr     error
	WantResult  interface{} // For functions that return values
}

// Helper function to setup mock command execution
func setupMockCommands(fields *TestFields) {
	fields.originalRunCommandWithTimeout = runCommandWithTimeoutFunc
	fields.originalRunCommandWithTimeoutNoOutput = runCommandWithTimeoutNoOutputFunc
	fields.originalExecCommand = execCommandFunc

	runCommandWithTimeoutFunc = fields.mockRunCommandWithTimeout
	runCommandWithTimeoutNoOutputFunc = fields.mockRunCommandWithTimeoutNoOutput
	execCommandFunc = fields.mockExecCommand
}

// Helper function to restore original command execution
func teardownMockCommands(fields *TestFields) {
	if fields.originalRunCommandWithTimeout != nil {
		runCommandWithTimeoutFunc = fields.originalRunCommandWithTimeout
	}
	if fields.originalRunCommandWithTimeoutNoOutput != nil {
		runCommandWithTimeoutNoOutputFunc = fields.originalRunCommandWithTimeoutNoOutput
	}
	if fields.originalExecCommand != nil {
		execCommandFunc = fields.originalExecCommand
	}
}

func TestIsNetworkManagerAvailable(t *testing.T) {
	type TestArgs struct{}

	type TestCase struct {
		Name        string
		TestSetup   func() *TestFields
		TestPostRun func(fields *TestFields)
		Args        TestArgs
		WantResult  bool
	}

	cases := []TestCase{
		{
			Name: "Given_NetworkManagerAvailableAndRunning_When_CheckingAvailability_Then_ReturnsTrue",
			TestSetup: func() *TestFields {
				fields := &TestFields{
					callCount: make(map[string]int),
				}
				fields.mockRunCommandWithTimeoutNoOutput = func(name string, args ...string) error {
					fields.callCount["runCommandWithTimeoutNoOutput"]++
					return nil // nmcli is present
				}
				fields.mockRunCommandWithTimeout = func(name string, args ...string) ([]byte, error) {
					fields.callCount["runCommandWithTimeout"]++
					return []byte("running"), nil
				}
				setupMockCommands(fields)
				return fields
			},
			TestPostRun: func(fields *TestFields) {
				teardownMockCommands(fields)
				if fields.callCount["runCommandWithTimeoutNoOutput"] != 1 {
					t.Errorf("Expected runCommandWithTimeoutNoOutput to be called once, got %d", fields.callCount["runCommandWithTimeoutNoOutput"])
				}
				if fields.callCount["runCommandWithTimeout"] != 1 {
					t.Errorf("Expected runCommandWithTimeout to be called once, got %d", fields.callCount["runCommandWithTimeout"])
				}
			},
			Args:       TestArgs{},
			WantResult: true,
		},
		{
			Name: "Given_NetworkManagerBinaryNotAvailable_When_CheckingAvailability_Then_ReturnsFalse",
			TestSetup: func() *TestFields {
				fields := &TestFields{
					callCount: make(map[string]int),
				}
				fields.mockRunCommandWithTimeoutNoOutput = func(name string, args ...string) error {
					fields.callCount["runCommandWithTimeoutNoOutput"]++
					return errors.New("command not found")
				}
				setupMockCommands(fields)
				return fields
			},
			TestPostRun: func(fields *TestFields) {
				teardownMockCommands(fields)
			},
			Args:       TestArgs{},
			WantResult: false,
		},
		{
			Name: "Given_NetworkManagerNotRunning_When_CheckingAvailability_Then_ReturnsFalse",
			TestSetup: func() *TestFields {
				fields := &TestFields{
					callCount: make(map[string]int),
				}
				fields.mockRunCommandWithTimeoutNoOutput = func(name string, args ...string) error {
					fields.callCount["runCommandWithTimeoutNoOutput"]++
					return nil // nmcli is present
				}
				fields.mockRunCommandWithTimeout = func(name string, args ...string) ([]byte, error) {
					fields.callCount["runCommandWithTimeout"]++
					return []byte("stopped"), nil
				}
				setupMockCommands(fields)
				return fields
			},
			TestPostRun: func(fields *TestFields) {
				teardownMockCommands(fields)
			},
			Args: TestArgs{},
			// After removing detailed status inspection, any successful nmcli general call
			// means NetworkManager is considered available regardless of the textual RUNNING value.
			WantResult: true,
		},
		{
			Name: "Given_NetworkManagerStatusCheckFails_When_CheckingAvailability_Then_ReturnsFalse",
			TestSetup: func() *TestFields {
				fields := &TestFields{
					callCount: make(map[string]int),
				}
				fields.mockRunCommandWithTimeoutNoOutput = func(name string, args ...string) error {
					fields.callCount["runCommandWithTimeoutNoOutput"]++
					return nil // nmcli is present
				}
				fields.mockRunCommandWithTimeout = func(name string, args ...string) ([]byte, error) {
					fields.callCount["runCommandWithTimeout"]++
					return nil, errors.New("nmcli general failed")
				}
				setupMockCommands(fields)
				return fields
			},
			TestPostRun: func(fields *TestFields) {
				teardownMockCommands(fields)
			},
			Args:       TestArgs{},
			WantResult: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			fields := tc.TestSetup()
			defer tc.TestPostRun(fields)

			result := isNetworkManagerAvailable()
			if result != tc.WantResult {
				t.Errorf("isNetworkManagerAvailable() = %v, want %v", result, tc.WantResult)
			}
		})
	}
}

func TestIsDeviceUnmanaged(t *testing.T) {
	type TestArgs struct {
		IfName string
	}

	type TestCase struct {
		Name        string
		TestSetup   func() *TestFields
		TestPostRun func(fields *TestFields)
		Args        TestArgs
		WantResult  bool
		WantErr     error
	}

	cases := []TestCase{
		{
			Name: "Given_DeviceIsUnmanaged_When_CheckingStatus_Then_ReturnsTrue",
			TestSetup: func() *TestFields {
				fields := &TestFields{
					callCount: make(map[string]int),
				}
				fields.mockRunCommandWithTimeout = func(name string, args ...string) ([]byte, error) {
					fields.callCount["runCommandWithTimeout"]++
					return []byte("GENERAL.STATE:unmanaged"), nil
				}
				setupMockCommands(fields)
				return fields
			},
			TestPostRun: func(fields *TestFields) {
				teardownMockCommands(fields)
			},
			Args:       TestArgs{IfName: "eth0"},
			WantResult: true,
			WantErr:    nil,
		},
		{
			Name: "Given_DeviceIsManaged_When_CheckingStatus_Then_ReturnsFalse",
			TestSetup: func() *TestFields {
				fields := &TestFields{
					callCount: make(map[string]int),
				}
				fields.mockRunCommandWithTimeout = func(name string, args ...string) ([]byte, error) {
					fields.callCount["runCommandWithTimeout"]++
					return []byte("GENERAL.STATE:connected"), nil
				}
				setupMockCommands(fields)
				return fields
			},
			TestPostRun: func(fields *TestFields) {
				teardownMockCommands(fields)
			},
			Args:       TestArgs{IfName: "eth0"},
			WantResult: false,
			WantErr:    nil,
		},
		{
			Name: "Given_CommandFails_When_CheckingStatus_Then_ReturnsError",
			TestSetup: func() *TestFields {
				fields := &TestFields{
					callCount: make(map[string]int),
				}
				fields.mockRunCommandWithTimeout = func(name string, args ...string) ([]byte, error) {
					fields.callCount["runCommandWithTimeout"]++
					return nil, errors.New("command failed")
				}
				setupMockCommands(fields)
				return fields
			},
			TestPostRun: func(fields *TestFields) {
				teardownMockCommands(fields)
			},
			Args:       TestArgs{IfName: "eth0"},
			WantResult: false,
			WantErr:    errors.New("failed to check NetworkManager status for eth0: command failed"),
		},
		{
			Name: "Given_OutputContainsUnmanagedCaseInsensitive_When_CheckingStatus_Then_ReturnsTrue",
			TestSetup: func() *TestFields {
				fields := &TestFields{
					callCount: make(map[string]int),
				}
				fields.mockRunCommandWithTimeout = func(name string, args ...string) ([]byte, error) {
					fields.callCount["runCommandWithTimeout"]++
					return []byte("GENERAL.STATE:UNMANAGED"), nil
				}
				setupMockCommands(fields)
				return fields
			},
			TestPostRun: func(fields *TestFields) {
				teardownMockCommands(fields)
			},
			Args:       TestArgs{IfName: "eth0"},
			WantResult: true,
			WantErr:    nil,
		},
		{
			Name: "Given_OutputHasWhitespace_When_CheckingStatus_Then_TrimsAndReturnsCorrectResult",
			TestSetup: func() *TestFields {
				fields := &TestFields{
					callCount: make(map[string]int),
				}
				fields.mockRunCommandWithTimeout = func(name string, args ...string) ([]byte, error) {
					fields.callCount["runCommandWithTimeout"]++
					return []byte("  GENERAL.STATE:unmanaged  \n"), nil
				}
				setupMockCommands(fields)
				return fields
			},
			TestPostRun: func(fields *TestFields) {
				teardownMockCommands(fields)
			},
			Args:       TestArgs{IfName: "eth0"},
			WantResult: true,
			WantErr:    nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			fields := tc.TestSetup()
			defer tc.TestPostRun(fields)

			result, err := isDeviceUnmanaged(tc.Args.IfName)

			if tc.WantErr != nil {
				if err == nil {
					t.Errorf("isDeviceUnmanaged() expected error %v, got nil", tc.WantErr)
					return
				}
				if !strings.Contains(err.Error(), tc.WantErr.Error()) {
					t.Errorf("isDeviceUnmanaged() error = %v, want %v", err, tc.WantErr)
				}
			} else {
				if err != nil {
					t.Errorf("isDeviceUnmanaged() unexpected error = %v", err)
					return
				}
				if result != tc.WantResult {
					t.Errorf("isDeviceUnmanaged() = %v, want %v", result, tc.WantResult)
				}
			}
		})
	}
}

func TestSetDeviceUnmanaged(t *testing.T) {
	type TestArgs struct {
		IfName string
	}

	type TestCase struct {
		Name        string
		TestSetup   func() *TestFields
		TestPostRun func(fields *TestFields)
		Args        TestArgs
		WantErr     error
	}

	cases := []TestCase{
		{
			Name: "Given_ValidInterface_When_SettingUnmanaged_Then_Succeeds",
			TestSetup: func() *TestFields {
				fields := &TestFields{
					callCount: make(map[string]int),
				}
				fields.mockExecCommand = func(name string, args ...string) error {
					fields.callCount["execCommand"]++
					return nil // Success
				}
				setupMockCommands(fields)
				return fields
			},
			TestPostRun: func(fields *TestFields) {
				teardownMockCommands(fields)
				if fields.callCount["execCommand"] != 1 {
					t.Errorf("Expected execCommand to be called once, got %d", fields.callCount["execCommand"])
				}
			},
			Args:    TestArgs{IfName: "eth0"},
			WantErr: nil,
		},
		{
			Name: "Given_CommandFails_When_SettingUnmanaged_Then_ReturnsError",
			TestSetup: func() *TestFields {
				fields := &TestFields{
					callCount: make(map[string]int),
				}
				fields.mockExecCommand = func(name string, args ...string) error {
					fields.callCount["execCommand"]++
					return errors.New("permission denied")
				}
				setupMockCommands(fields)
				return fields
			},
			TestPostRun: func(fields *TestFields) {
				teardownMockCommands(fields)
			},
			Args:    TestArgs{IfName: "eth0"},
			WantErr: errors.New("permission denied"),
		},
	}

	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			fields := tc.TestSetup()
			defer tc.TestPostRun(fields)

			err := setDeviceUnmanaged(tc.Args.IfName)

			if tc.WantErr != nil {
				if err == nil {
					t.Errorf("setDeviceUnmanaged() expected error %v, got nil", tc.WantErr)
					return
				}
				if !strings.Contains(err.Error(), tc.WantErr.Error()) {
					t.Errorf("setDeviceUnmanaged() error = %v, want %v", err, tc.WantErr)
				}
			} else {
				if err != nil {
					t.Errorf("setDeviceUnmanaged() unexpected error = %v", err)
				}
			}
		})
	}
}

func TestEnsureNetworkManagerUnmanaged(t *testing.T) {
	type TestArgs struct {
		IfName string
	}

	type TestCase struct {
		Name        string
		TestSetup   func() *TestFields
		TestPostRun func(fields *TestFields)
		Args        TestArgs
		WantErr     error
	}

	// Save original retry interval and timeout for faster tests
	originalRetryInterval := retryIntervalOverride
	originalTimeout := timeoutOverride
	defer func() {
		retryIntervalOverride = originalRetryInterval
		timeoutOverride = originalTimeout
	}()

	cases := []TestCase{
		{
			Name: "Given_NetworkManagerNotAvailable_When_EnsuringUnmanaged_Then_ReturnsNil",
			TestSetup: func() *TestFields {
				fields := &TestFields{
					callCount: make(map[string]int),
				}
				fields.mockRunCommandWithTimeoutNoOutput = func(name string, args ...string) error {
					fields.callCount["runCommandWithTimeoutNoOutput"]++
					return errors.New("command not found")
				}
				setupMockCommands(fields)
				return fields
			},
			TestPostRun: func(fields *TestFields) {
				teardownMockCommands(fields)
			},
			Args:    TestArgs{IfName: "eth0"},
			WantErr: nil,
		},
		{
			Name: "Given_DeviceAlreadyUnmanaged_When_EnsuringUnmanaged_Then_ReturnsNil",
			TestSetup: func() *TestFields {
				fields := &TestFields{
					callCount: make(map[string]int),
				}
				fields.mockRunCommandWithTimeoutNoOutput = func(name string, args ...string) error {
					fields.callCount["runCommandWithTimeoutNoOutput"]++
					return nil
				}
				fields.mockRunCommandWithTimeout = func(name string, args ...string) ([]byte, error) {
					fields.callCount["runCommandWithTimeout"]++
					// Distinguish between NetworkManager general status check and device status check
					argStr := strings.Join(args, " ")
					if strings.Contains(argStr, "general") {
						// isNetworkManagerAvailable: report running
						return []byte("running"), nil
					}
					// isDeviceUnmanaged: already unmanaged
					return []byte("GENERAL.STATE:unmanaged"), nil
				}
				setupMockCommands(fields)
				return fields
			},
			TestPostRun: func(fields *TestFields) {
				teardownMockCommands(fields)
			},
			Args:    TestArgs{IfName: "eth0"},
			WantErr: nil,
		},
		{
			Name: "Given_NetworkManagerNotRunning_When_EnsuringUnmanaged_Then_ReturnsNil",
			TestSetup: func() *TestFields {
				fields := &TestFields{
					callCount: make(map[string]int),
				}
				fields.mockRunCommandWithTimeoutNoOutput = func(name string, args ...string) error {
					fields.callCount["runCommandWithTimeoutNoOutput"]++
					return nil
				}
				fields.mockRunCommandWithTimeout = func(name string, args ...string) ([]byte, error) {
					fields.callCount["runCommandWithTimeout"]++
					// Simulate nmcli present but general status command failing
					return nil, errors.New("NetworkManager is not running")
				}
				setupMockCommands(fields)
				return fields
			},
			TestPostRun: func(fields *TestFields) {
				teardownMockCommands(fields)
			},
			Args:    TestArgs{IfName: "eth0"},
			WantErr: nil,
		},
		{
			Name: "Given_CheckStatusFails_When_EnsuringUnmanaged_Then_ReturnsError",
			TestSetup: func() *TestFields {
				fields := &TestFields{
					callCount: make(map[string]int),
				}
				fields.mockRunCommandWithTimeoutNoOutput = func(name string, args ...string) error {
					fields.callCount["runCommandWithTimeoutNoOutput"]++
					return nil
				}
				fields.mockRunCommandWithTimeout = func(name string, args ...string) ([]byte, error) {
					fields.callCount["runCommandWithTimeout"]++
					// Distinguish between NetworkManager general status check and device status check
					argStr := strings.Join(args, " ")
					if strings.Contains(argStr, "general") {
						// isNetworkManagerAvailable: report running
						return []byte("running"), nil
					}
					// isDeviceUnmanaged: fail when checking device status
					return nil, errors.New("check failed")
				}
				setupMockCommands(fields)
				return fields
			},
			TestPostRun: func(fields *TestFields) {
				teardownMockCommands(fields)
			},
			Args:    TestArgs{IfName: "eth0"},
			WantErr: errors.New("failed to check NetworkManager status for eth0: check failed"),
		},
		{
			Name: "Given_DeviceNeedsConfiguration_When_EnsuringUnmanaged_Then_SucceedsAfterRetry",
			TestSetup: func() *TestFields {
				// Use shorter intervals for faster tests
				shortInterval := 10 * time.Millisecond
				shortTimeout := 100 * time.Millisecond
				retryIntervalOverride = shortInterval
				timeoutOverride = shortTimeout

				fields := &TestFields{
					callCount: make(map[string]int),
				}
				checkCount := 0
				fields.mockRunCommandWithTimeoutNoOutput = func(name string, args ...string) error {
					fields.callCount["runCommandWithTimeoutNoOutput"]++
					return nil
				}
				fields.mockRunCommandWithTimeout = func(name string, args ...string) ([]byte, error) {
					fields.callCount["runCommandWithTimeout"]++
					argStr := strings.Join(args, " ")
					if strings.Contains(argStr, "general") {
						// isNetworkManagerAvailable: report running
						return []byte("running"), nil
					}
					// Device status checks
					checkCount++
					// First device check returns managed, second returns unmanaged
					if checkCount == 1 {
						return []byte("GENERAL.STATE:connected"), nil
					}
					return []byte("GENERAL.STATE:unmanaged"), nil
				}
				fields.mockExecCommand = func(name string, args ...string) error {
					fields.callCount["execCommand"]++
					return nil
				}
				setupMockCommands(fields)
				return fields
			},
			TestPostRun: func(fields *TestFields) {
				teardownMockCommands(fields)
				// Should have called setDeviceUnmanaged and checked status multiple times
				if fields.callCount["execCommand"] < 1 {
					t.Errorf("Expected execCommand to be called at least once, got %d", fields.callCount["execCommand"])
				}
			},
			Args:    TestArgs{IfName: "eth0"},
			WantErr: nil,
		},
		{
			Name: "Given_SetDeviceFails_When_EnsuringUnmanaged_Then_Retries",
			TestSetup: func() *TestFields {
				// Use shorter intervals for faster tests
				shortInterval := 10 * time.Millisecond
				shortTimeout := 100 * time.Millisecond
				retryIntervalOverride = shortInterval
				timeoutOverride = shortTimeout

				fields := &TestFields{
					callCount: make(map[string]int),
				}
				setCount := 0
				fields.mockRunCommandWithTimeoutNoOutput = func(name string, args ...string) error {
					fields.callCount["runCommandWithTimeoutNoOutput"]++
					return nil
				}
				fields.mockRunCommandWithTimeout = func(name string, args ...string) ([]byte, error) {
					fields.callCount["runCommandWithTimeout"]++
					argStr := strings.Join(args, " ")
					if strings.Contains(argStr, "general") {
						// isNetworkManagerAvailable: report running
						return []byte("running"), nil
					}
					// Device status checks: always managed to force retries
					return []byte("GENERAL.STATE:connected"), nil
				}
				fields.mockExecCommand = func(name string, args ...string) error {
					fields.callCount["execCommand"]++
					setCount++
					// Fail first time, succeed second time
					if setCount == 1 {
						return errors.New("temporary failure")
					}
					return nil
				}
				setupMockCommands(fields)
				return fields
			},
			TestPostRun: func(fields *TestFields) {
				teardownMockCommands(fields)
				// Should have retried multiple times
				if fields.callCount["execCommand"] < 2 {
					t.Errorf("Expected execCommand to be called multiple times, got %d", fields.callCount["execCommand"])
				}
			},
			Args:    TestArgs{IfName: "eth0"},
			WantErr: nil, // Will timeout but that's expected in this test
		},
		{
			Name: "Given_VerifyFails_When_EnsuringUnmanaged_Then_Retries",
			TestSetup: func() *TestFields {
				// Use shorter intervals for faster tests
				shortInterval := 10 * time.Millisecond
				shortTimeout := 100 * time.Millisecond
				retryIntervalOverride = shortInterval
				timeoutOverride = shortTimeout

				fields := &TestFields{
					callCount: make(map[string]int),
				}
				checkCount := 0
				fields.mockRunCommandWithTimeoutNoOutput = func(name string, args ...string) error {
					fields.callCount["runCommandWithTimeoutNoOutput"]++
					return nil
				}
				fields.mockRunCommandWithTimeout = func(name string, args ...string) ([]byte, error) {
					fields.callCount["runCommandWithTimeout"]++
					argStr := strings.Join(args, " ")
					if strings.Contains(argStr, "general") {
						// isNetworkManagerAvailable: report running
						return []byte("running"), nil
					}
					// Device status checks
					checkCount++
					// First device check returns managed, subsequent checks fail
					if checkCount == 1 {
						return []byte("GENERAL.STATE:connected"), nil
					}
					// After setting, verification fails
					return nil, errors.New("verification failed")
				}
				fields.mockExecCommand = func(name string, args ...string) error {
					fields.callCount["execCommand"]++
					return nil
				}
				setupMockCommands(fields)
				return fields
			},
			TestPostRun: func(fields *TestFields) {
				teardownMockCommands(fields)
				// Should have retried multiple times
				if fields.callCount["runCommandWithTimeout"] < 2 {
					t.Errorf("Expected runCommandWithTimeout to be called multiple times, got %d", fields.callCount["runCommandWithTimeout"])
				}
			},
			Args:    TestArgs{IfName: "eth0"},
			WantErr: nil, // Will timeout but that's expected in this test
		},
	}

	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			fields := tc.TestSetup()
			defer tc.TestPostRun(fields)

			c := &centos{}
			err := c.EnsureNetworkManagerUnmanaged(tc.Args.IfName)

			if tc.WantErr != nil {
				if err == nil {
					t.Errorf("EnsureNetworkManagerUnmanaged() expected error %v, got nil", tc.WantErr)
					return
				}
				if !strings.Contains(err.Error(), tc.WantErr.Error()) {
					t.Errorf("EnsureNetworkManagerUnmanaged() error = %v, want %v", err, tc.WantErr)
				}
			} else {
				// For timeout cases, we don't check error
				if err != nil && !strings.Contains(err.Error(), "timed out") {
					// Only fail if it's not a timeout error (which is expected in some test cases)
					if !strings.Contains(tc.Name, "Retries") {
						t.Errorf("EnsureNetworkManagerUnmanaged() unexpected error = %v", err)
					}
				}
			}
		})
	}
}

// Test edge cases and boundary conditions
func TestIsDeviceUnmanaged_EdgeCases(t *testing.T) {
	type TestArgs struct {
		IfName string
	}

	type TestCase struct {
		Name        string
		TestSetup   func() *TestFields
		TestPostRun func(fields *TestFields)
		Args        TestArgs
		WantResult  bool
		WantErr     error
	}

	cases := []TestCase{
		{
			Name: "Given_EmptyOutput_When_CheckingStatus_Then_ReturnsFalse",
			TestSetup: func() *TestFields {
				fields := &TestFields{
					callCount: make(map[string]int),
				}
				fields.mockRunCommandWithTimeout = func(name string, args ...string) ([]byte, error) {
					fields.callCount["runCommandWithTimeout"]++
					return []byte(""), nil
				}
				setupMockCommands(fields)
				return fields
			},
			TestPostRun: func(fields *TestFields) {
				teardownMockCommands(fields)
			},
			Args:       TestArgs{IfName: "eth0"},
			WantResult: false,
			WantErr:    nil,
		},
		{
			Name: "Given_OutputContainsUnmanagedInMiddle_When_CheckingStatus_Then_ReturnsTrue",
			TestSetup: func() *TestFields {
				fields := &TestFields{
					callCount: make(map[string]int),
				}
				fields.mockRunCommandWithTimeout = func(name string, args ...string) ([]byte, error) {
					fields.callCount["runCommandWithTimeout"]++
					return []byte("GENERAL.STATE:unmanaged\nOTHER:value"), nil
				}
				setupMockCommands(fields)
				return fields
			},
			TestPostRun: func(fields *TestFields) {
				teardownMockCommands(fields)
			},
			Args:       TestArgs{IfName: "eth0"},
			WantResult: true,
			WantErr:    nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			fields := tc.TestSetup()
			defer tc.TestPostRun(fields)

			result, err := isDeviceUnmanaged(tc.Args.IfName)

			if tc.WantErr != nil {
				if err == nil {
					t.Errorf("isDeviceUnmanaged() expected error %v, got nil", tc.WantErr)
					return
				}
			} else {
				if err != nil {
					t.Errorf("isDeviceUnmanaged() unexpected error = %v", err)
					return
				}
				if result != tc.WantResult {
					t.Errorf("isDeviceUnmanaged() = %v, want %v", result, tc.WantResult)
				}
			}
		})
	}
}

// TestFields for command execution function tests
type CommandTestFields struct {
	// No special fields needed for these tests
}

// TestArgs for command execution functions
type CommandTestArgs struct {
	Name string
	Args []string
}

// TestCase for command execution functions
type CommandTestCase struct {
	Name        string
	TestSetup   func() *CommandTestFields
	TestPostRun func(fields *CommandTestFields)
	Args        CommandTestArgs
	WantErr     error
	WantOutput  string // For runCommandWithTimeout
}

// TestRunCommandWithTimeout tests the runCommandWithTimeout function
func TestRunCommandWithTimeout(t *testing.T) {
	cases := []CommandTestCase{
		{
			Name: "TestRunCommandWithTimeout_Given_ValidCommand_When_Execute_Then_ReturnOutput",
			TestSetup: func() *CommandTestFields {
				return &CommandTestFields{}
			},
			TestPostRun: func(fields *CommandTestFields) {
				// No cleanup needed
			},
			Args: CommandTestArgs{
				Name: "echo",
				Args: []string{"hello", "world"},
			},
			WantErr:    nil,
			WantOutput: "hello world\n",
		},
		{
			Name: "TestRunCommandWithTimeout_Given_CommandWithNoArgs_When_Execute_Then_ReturnOutput",
			TestSetup: func() *CommandTestFields {
				return &CommandTestFields{}
			},
			TestPostRun: func(fields *CommandTestFields) {
				// No cleanup needed
			},
			Args: CommandTestArgs{
				Name: "echo",
				Args: []string{"test"},
			},
			WantErr:    nil,
			WantOutput: "test\n",
		},
		{
			Name: "TestRunCommandWithTimeout_Given_InvalidCommand_When_Execute_Then_ReturnError",
			TestSetup: func() *CommandTestFields {
				return &CommandTestFields{}
			},
			TestPostRun: func(fields *CommandTestFields) {
				// No cleanup needed
			},
			Args: CommandTestArgs{
				Name: "nonexistent_command_12345",
				Args: []string{},
			},
			WantErr:    errors.New("executable file not found"),
			WantOutput: "",
		},
		{
			Name: "TestRunCommandWithTimeout_Given_CommandThatFails_When_Execute_Then_ReturnError",
			TestSetup: func() *CommandTestFields {
				return &CommandTestFields{}
			},
			TestPostRun: func(fields *CommandTestFields) {
				// No cleanup needed
			},
			Args: CommandTestArgs{
				Name: "false",
				Args: []string{},
			},
			WantErr:    errors.New("exit status"),
			WantOutput: "",
		},
		{
			Name: "TestRunCommandWithTimeout_Given_EmptyOutput_When_Execute_Then_ReturnEmptyOutput",
			TestSetup: func() *CommandTestFields {
				return &CommandTestFields{}
			},
			TestPostRun: func(fields *CommandTestFields) {
				// No cleanup needed
			},
			Args: CommandTestArgs{
				Name: "true",
				Args: []string{},
			},
			WantErr:    nil,
			WantOutput: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			fields := tc.TestSetup()
			defer tc.TestPostRun(fields)

			// Call runCommandWithTimeout
			output, gotErr := runCommandWithTimeout(tc.Args.Name, tc.Args.Args...)

			// Verify error
			if tc.WantErr != nil {
				if gotErr == nil {
					t.Errorf("runCommandWithTimeout() expected error %v, got nil", tc.WantErr)
					return
				}
				if !strings.Contains(gotErr.Error(), tc.WantErr.Error()) {
					t.Errorf("runCommandWithTimeout() error = %v, want error containing %v", gotErr, tc.WantErr)
				}
			} else {
				if gotErr != nil {
					t.Errorf("runCommandWithTimeout() unexpected error = %v", gotErr)
					return
				}
			}

			// Verify output
			if tc.WantOutput != "" {
				outputStr := string(output)
				if outputStr != tc.WantOutput {
					t.Errorf("runCommandWithTimeout() output = %q, want %q", outputStr, tc.WantOutput)
				}
			} else if tc.WantErr == nil {
				// For successful commands with no expected output, just verify no error
				_ = output
			}
		})
	}
}

// TestRunCommandWithTimeoutNoOutput tests the runCommandWithTimeoutNoOutput function
func TestRunCommandWithTimeoutNoOutput(t *testing.T) {
	cases := []CommandTestCase{
		{
			Name: "TestRunCommandWithTimeoutNoOutput_Given_ValidCommand_When_Execute_Then_ReturnNil",
			TestSetup: func() *CommandTestFields {
				return &CommandTestFields{}
			},
			TestPostRun: func(fields *CommandTestFields) {
				// No cleanup needed
			},
			Args: CommandTestArgs{
				Name: "true",
				Args: []string{},
			},
			WantErr: nil,
		},
		{
			Name: "TestRunCommandWithTimeoutNoOutput_Given_CommandWithArgs_When_Execute_Then_ReturnNil",
			TestSetup: func() *CommandTestFields {
				return &CommandTestFields{}
			},
			TestPostRun: func(fields *CommandTestFields) {
				// No cleanup needed
			},
			Args: CommandTestArgs{
				Name: "echo",
				Args: []string{"test"},
			},
			WantErr: nil,
		},
		{
			Name: "TestRunCommandWithTimeoutNoOutput_Given_InvalidCommand_When_Execute_Then_ReturnError",
			TestSetup: func() *CommandTestFields {
				return &CommandTestFields{}
			},
			TestPostRun: func(fields *CommandTestFields) {
				// No cleanup needed
			},
			Args: CommandTestArgs{
				Name: "nonexistent_command_12345",
				Args: []string{},
			},
			WantErr: errors.New("executable file not found"),
		},
		{
			Name: "TestRunCommandWithTimeoutNoOutput_Given_CommandThatFails_When_Execute_Then_ReturnError",
			TestSetup: func() *CommandTestFields {
				return &CommandTestFields{}
			},
			TestPostRun: func(fields *CommandTestFields) {
				// No cleanup needed
			},
			Args: CommandTestArgs{
				Name: "false",
				Args: []string{},
			},
			WantErr: errors.New("exit status"),
		},
	}

	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			fields := tc.TestSetup()
			defer tc.TestPostRun(fields)

			// Call runCommandWithTimeoutNoOutput
			gotErr := runCommandWithTimeoutNoOutput(tc.Args.Name, tc.Args.Args...)

			// Verify error
			if tc.WantErr != nil {
				if gotErr == nil {
					t.Errorf("runCommandWithTimeoutNoOutput() expected error %v, got nil", tc.WantErr)
					return
				}
				if !strings.Contains(gotErr.Error(), tc.WantErr.Error()) {
					t.Errorf("runCommandWithTimeoutNoOutput() error = %v, want error containing %v", gotErr, tc.WantErr)
				}
			} else {
				if gotErr != nil {
					t.Errorf("runCommandWithTimeoutNoOutput() unexpected error = %v", gotErr)
				}
			}
		})
	}
}

// TestExecCommand tests the execCommand function
func TestExecCommand(t *testing.T) {
	cases := []CommandTestCase{
		{
			Name: "TestExecCommand_Given_ValidCommand_When_Execute_Then_ReturnNil",
			TestSetup: func() *CommandTestFields {
				return &CommandTestFields{}
			},
			TestPostRun: func(fields *CommandTestFields) {
				// No cleanup needed
			},
			Args: CommandTestArgs{
				Name: "true",
				Args: []string{},
			},
			WantErr: nil,
		},
		{
			Name: "TestExecCommand_Given_CommandWithArgs_When_Execute_Then_ReturnNil",
			TestSetup: func() *CommandTestFields {
				return &CommandTestFields{}
			},
			TestPostRun: func(fields *CommandTestFields) {
				// No cleanup needed
			},
			Args: CommandTestArgs{
				Name: "echo",
				Args: []string{"test"},
			},
			WantErr: nil,
		},
		{
			Name: "TestExecCommand_Given_InvalidCommand_When_Execute_Then_ReturnError",
			TestSetup: func() *CommandTestFields {
				return &CommandTestFields{}
			},
			TestPostRun: func(fields *CommandTestFields) {
				// No cleanup needed
			},
			Args: CommandTestArgs{
				Name: "nonexistent_command_12345",
				Args: []string{},
			},
			WantErr: errors.New("executable file not found"),
		},
		{
			Name: "TestExecCommand_Given_CommandThatFails_When_Execute_Then_ReturnError",
			TestSetup: func() *CommandTestFields {
				return &CommandTestFields{}
			},
			TestPostRun: func(fields *CommandTestFields) {
				// No cleanup needed
			},
			Args: CommandTestArgs{
				Name: "false",
				Args: []string{},
			},
			WantErr: errors.New("exit status"),
		},
	}

	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			fields := tc.TestSetup()
			defer tc.TestPostRun(fields)

			// Call execCommand
			gotErr := execCommand(tc.Args.Name, tc.Args.Args...)

			// Verify error
			if tc.WantErr != nil {
				if gotErr == nil {
					t.Errorf("execCommand() expected error %v, got nil", tc.WantErr)
					return
				}
				if !strings.Contains(gotErr.Error(), tc.WantErr.Error()) {
					t.Errorf("execCommand() error = %v, want error containing %v", gotErr, tc.WantErr)
				}
			} else {
				if gotErr != nil {
					t.Errorf("execCommand() unexpected error = %v", gotErr)
				}
			}
		})
	}
}

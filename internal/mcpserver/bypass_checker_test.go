package mcpserver

import (
	"fmt"
	"reflect"
	"testing"
)

func TestNewBypassIPChecker(t *testing.T) {
	tests := []struct {
		name         string
		bypassRanges []string
		verboseLog   bool
		wantErr      bool
		wantRanges   []string
	}{
		{
			name:         "valid single CIDR",
			bypassRanges: []string{"10.0.0.0/24"},
			verboseLog:   false,
			wantErr:      false,
			wantRanges:   []string{"10.0.0.0/24"},
		},
		{
			name:         "valid multiple CIDRs",
			bypassRanges: []string{"10.0.0.0/24", "192.168.1.0/24", "172.16.0.0/16"},
			verboseLog:   false,
			wantErr:      false,
			wantRanges:   []string{"10.0.0.0/24", "192.168.1.0/24", "172.16.0.0/16"},
		},
		{
			name:         "single IP without CIDR",
			bypassRanges: []string{"10.0.0.1"},
			verboseLog:   false,
			wantErr:      false,
			wantRanges:   []string{"10.0.0.1/32"},
		},
		{
			name:         "IPv6 CIDR",
			bypassRanges: []string{"2001:db8::/32"},
			verboseLog:   false,
			wantErr:      false,
			wantRanges:   []string{"2001:db8::/32"},
		},
		{
			name:         "IPv6 single address",
			bypassRanges: []string{"2001:db8::1"},
			verboseLog:   false,
			wantErr:      false,
			wantRanges:   []string{"2001:db8::1/128"},
		},
		{
			name:         "invalid CIDR",
			bypassRanges: []string{"10.0.0.0/33"},
			verboseLog:   false,
			wantErr:      true,
			wantRanges:   nil,
		},
		{
			name:         "invalid IP",
			bypassRanges: []string{"not-an-ip"},
			verboseLog:   false,
			wantErr:      true,
			wantRanges:   nil,
		},
		{
			name:         "empty ranges",
			bypassRanges: []string{},
			verboseLog:   false,
			wantErr:      false,
			wantRanges:   []string{},
		},
		{
			name:         "with verbose logging",
			bypassRanges: []string{"10.0.0.0/24"},
			verboseLog:   true,
			wantErr:      false,
			wantRanges:   []string{"10.0.0.0/24"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			checker, err := NewBypassIPChecker(tt.bypassRanges, tt.verboseLog)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewBypassIPChecker() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if err != nil {
				return
			}

			gotRanges := checker.GetBypassRanges()
			if !reflect.DeepEqual(gotRanges, tt.wantRanges) {
				t.Errorf("NewBypassIPChecker() ranges = %v, want %v", gotRanges, tt.wantRanges)
			}

			if checker.verboseLog != tt.verboseLog {
				t.Errorf("NewBypassIPChecker() verboseLog = %v, want %v", checker.verboseLog, tt.verboseLog)
			}
		})
	}
}

func TestBypassIPCheckerImpl_ShouldBypass(t *testing.T) {
	tests := []struct {
		name         string
		bypassRanges []string
		testIP       string
		wantBypass   bool
	}{
		// IPv4 tests
		{
			name:         "IPv4 within /24 range",
			bypassRanges: []string{"10.0.0.0/24"},
			testIP:       "10.0.0.100",
			wantBypass:   true,
		},
		{
			name:         "IPv4 outside /24 range",
			bypassRanges: []string{"10.0.0.0/24"},
			testIP:       "10.0.1.100",
			wantBypass:   false,
		},
		{
			name:         "IPv4 exact match /32",
			bypassRanges: []string{"10.0.0.100/32"},
			testIP:       "10.0.0.100",
			wantBypass:   true,
		},
		{
			name:         "IPv4 no match /32",
			bypassRanges: []string{"10.0.0.100/32"},
			testIP:       "10.0.0.101",
			wantBypass:   false,
		},
		{
			name:         "IPv4 within /16 range",
			bypassRanges: []string{"172.16.0.0/16"},
			testIP:       "172.16.100.50",
			wantBypass:   true,
		},
		{
			name:         "IPv4 within /8 range",
			bypassRanges: []string{"10.0.0.0/8"},
			testIP:       "10.100.50.25",
			wantBypass:   true,
		},
		{
			name:         "IPv4 multiple ranges match",
			bypassRanges: []string{"10.0.0.0/24", "192.168.1.0/24"},
			testIP:       "192.168.1.100",
			wantBypass:   true,
		},
		{
			name:         "IPv4 multiple ranges no match",
			bypassRanges: []string{"10.0.0.0/24", "192.168.1.0/24"},
			testIP:       "172.16.0.1",
			wantBypass:   false,
		},
		// IPv6 tests
		{
			name:         "IPv6 within range",
			bypassRanges: []string{"2001:db8::/32"},
			testIP:       "2001:db8:85a3::8a2e:370:7334",
			wantBypass:   true,
		},
		{
			name:         "IPv6 outside range",
			bypassRanges: []string{"2001:db8::/32"},
			testIP:       "2001:db9:85a3::8a2e:370:7334",
			wantBypass:   false,
		},
		{
			name:         "IPv6 exact match",
			bypassRanges: []string{"2001:db8::1/128"},
			testIP:       "2001:db8::1",
			wantBypass:   true,
		},
		{
			name:         "IPv6 no match",
			bypassRanges: []string{"2001:db8::1/128"},
			testIP:       "2001:db8::2",
			wantBypass:   false,
		},
		// Edge cases
		{
			name:         "empty IP string",
			bypassRanges: []string{"10.0.0.0/24"},
			testIP:       "",
			wantBypass:   false,
		},
		{
			name:         "invalid IP string",
			bypassRanges: []string{"10.0.0.0/24"},
			testIP:       "not-an-ip",
			wantBypass:   false,
		},
		{
			name:         "no bypass ranges",
			bypassRanges: []string{},
			testIP:       "10.0.0.1",
			wantBypass:   false,
		},
		// Localhost tests
		{
			name:         "localhost IPv4",
			bypassRanges: []string{"127.0.0.0/8"},
			testIP:       "127.0.0.1",
			wantBypass:   true,
		},
		{
			name:         "localhost IPv6",
			bypassRanges: []string{"::1/128"},
			testIP:       "::1",
			wantBypass:   true,
		},
		// Private network ranges
		{
			name:         "private network class A",
			bypassRanges: []string{"10.0.0.0/8"},
			testIP:       "10.255.255.254",
			wantBypass:   true,
		},
		{
			name:         "private network class B",
			bypassRanges: []string{"172.16.0.0/12"},
			testIP:       "172.31.255.254",
			wantBypass:   true,
		},
		{
			name:         "private network class C",
			bypassRanges: []string{"192.168.0.0/16"},
			testIP:       "192.168.255.254",
			wantBypass:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			checker, err := NewBypassIPChecker(tt.bypassRanges, false)
			if err != nil {
				t.Fatalf("Failed to create checker: %v", err)
			}

			gotBypass := checker.ShouldBypass(tt.testIP)
			if gotBypass != tt.wantBypass {
				t.Errorf("ShouldBypass(%q) = %v, want %v", tt.testIP, gotBypass, tt.wantBypass)
			}
		})
	}
}

func TestBypassIPCheckerImpl_AddBypassRange(t *testing.T) {
	tests := []struct {
		name          string
		initialRanges []string
		addRange      string
		wantErr       bool
		wantRanges    []string
	}{
		{
			name:          "add valid CIDR to empty",
			initialRanges: []string{},
			addRange:      "10.0.0.0/24",
			wantErr:       false,
			wantRanges:    []string{"10.0.0.0/24"},
		},
		{
			name:          "add valid CIDR to existing",
			initialRanges: []string{"10.0.0.0/24"},
			addRange:      "192.168.1.0/24",
			wantErr:       false,
			wantRanges:    []string{"10.0.0.0/24", "192.168.1.0/24"},
		},
		{
			name:          "add single IP",
			initialRanges: []string{"10.0.0.0/24"},
			addRange:      "192.168.1.1",
			wantErr:       false,
			wantRanges:    []string{"10.0.0.0/24", "192.168.1.1/32"},
		},
		{
			name:          "add IPv6 CIDR",
			initialRanges: []string{},
			addRange:      "2001:db8::/32",
			wantErr:       false,
			wantRanges:    []string{"2001:db8::/32"},
		},
		{
			name:          "add IPv6 single address",
			initialRanges: []string{},
			addRange:      "2001:db8::1",
			wantErr:       false,
			wantRanges:    []string{"2001:db8::1/128"},
		},
		{
			name:          "add duplicate range",
			initialRanges: []string{"10.0.0.0/24"},
			addRange:      "10.0.0.0/24",
			wantErr:       false,
			wantRanges:    []string{"10.0.0.0/24"}, // Should not duplicate
		},
		{
			name:          "add invalid CIDR",
			initialRanges: []string{"10.0.0.0/24"},
			addRange:      "10.0.0.0/33",
			wantErr:       true,
			wantRanges:    []string{"10.0.0.0/24"}, // Should remain unchanged
		},
		{
			name:          "add invalid IP",
			initialRanges: []string{"10.0.0.0/24"},
			addRange:      "not-an-ip",
			wantErr:       true,
			wantRanges:    []string{"10.0.0.0/24"}, // Should remain unchanged
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			checker, err := NewBypassIPChecker(tt.initialRanges, false)
			if err != nil {
				t.Fatalf("Failed to create checker: %v", err)
			}

			err = checker.AddBypassRange(tt.addRange)
			if (err != nil) != tt.wantErr {
				t.Errorf("AddBypassRange() error = %v, wantErr %v", err, tt.wantErr)
			}

			gotRanges := checker.GetBypassRanges()
			if !reflect.DeepEqual(gotRanges, tt.wantRanges) {
				t.Errorf("After AddBypassRange(), ranges = %v, want %v", gotRanges, tt.wantRanges)
			}
		})
	}
}

func TestBypassIPCheckerImpl_RemoveBypassRange(t *testing.T) {
	tests := []struct {
		name          string
		initialRanges []string
		removeRange   string
		wantErr       bool
		wantRanges    []string
	}{
		{
			name:          "remove existing range",
			initialRanges: []string{"10.0.0.0/24", "192.168.1.0/24"},
			removeRange:   "10.0.0.0/24",
			wantErr:       false,
			wantRanges:    []string{"192.168.1.0/24"},
		},
		{
			name:          "remove last range",
			initialRanges: []string{"10.0.0.0/24"},
			removeRange:   "10.0.0.0/24",
			wantErr:       false,
			wantRanges:    []string{},
		},
		{
			name:          "remove from middle",
			initialRanges: []string{"10.0.0.0/24", "192.168.1.0/24", "172.16.0.0/16"},
			removeRange:   "192.168.1.0/24",
			wantErr:       false,
			wantRanges:    []string{"10.0.0.0/24", "172.16.0.0/16"},
		},
		{
			name:          "remove non-existent range",
			initialRanges: []string{"10.0.0.0/24"},
			removeRange:   "192.168.1.0/24",
			wantErr:       true,
			wantRanges:    []string{"10.0.0.0/24"},
		},
		{
			name:          "remove from empty",
			initialRanges: []string{},
			removeRange:   "10.0.0.0/24",
			wantErr:       true,
			wantRanges:    []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			checker, err := NewBypassIPChecker(tt.initialRanges, false)
			if err != nil {
				t.Fatalf("Failed to create checker: %v", err)
			}

			err = checker.RemoveBypassRange(tt.removeRange)
			if (err != nil) != tt.wantErr {
				t.Errorf("RemoveBypassRange() error = %v, wantErr %v", err, tt.wantErr)
			}

			gotRanges := checker.GetBypassRanges()
			if !reflect.DeepEqual(gotRanges, tt.wantRanges) {
				t.Errorf("After RemoveBypassRange(), ranges = %v, want %v", gotRanges, tt.wantRanges)
			}
		})
	}
}

func TestBypassIPCheckerImpl_GetBypassRanges(t *testing.T) {
	tests := []struct {
		name          string
		initialRanges []string
		wantRanges    []string
	}{
		{
			name:          "get empty ranges",
			initialRanges: []string{},
			wantRanges:    []string{},
		},
		{
			name:          "get single range",
			initialRanges: []string{"10.0.0.0/24"},
			wantRanges:    []string{"10.0.0.0/24"},
		},
		{
			name:          "get multiple ranges",
			initialRanges: []string{"10.0.0.0/24", "192.168.1.0/24", "172.16.0.0/16"},
			wantRanges:    []string{"10.0.0.0/24", "192.168.1.0/24", "172.16.0.0/16"},
		},
		{
			name:          "get after modification",
			initialRanges: []string{"10.0.0.0/24"},
			wantRanges:    []string{"10.0.0.0/24"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			checker, err := NewBypassIPChecker(tt.initialRanges, false)
			if err != nil {
				t.Fatalf("Failed to create checker: %v", err)
			}

			gotRanges := checker.GetBypassRanges()
			if !reflect.DeepEqual(gotRanges, tt.wantRanges) {
				t.Errorf("GetBypassRanges() = %v, want %v", gotRanges, tt.wantRanges)
			}

			// Verify returned slice is a copy (modification test)
			if len(gotRanges) > 0 {
				originalFirst := gotRanges[0]
				gotRanges[0] = "modified"

				secondGet := checker.GetBypassRanges()
				if secondGet[0] != originalFirst {
					t.Errorf("GetBypassRanges() returned reference instead of copy")
				}
			}
		})
	}
}

func TestBypassIPCheckerImpl_ConcurrentAccess(t *testing.T) {
	checker, err := NewBypassIPChecker([]string{"10.0.0.0/24"}, false)
	if err != nil {
		t.Fatalf("Failed to create checker: %v", err)
	}

	// Test concurrent reads
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func() {
			for j := 0; j < 100; j++ {
				checker.ShouldBypass("10.0.0.1")
				checker.GetBypassRanges()
			}
			done <- true
		}()
	}

	// Test concurrent writes
	for i := 0; i < 5; i++ {
		go func(n int) {
			cidr := fmt.Sprintf("192.168.%d.0/24", n)
			for j := 0; j < 50; j++ {
				if err := checker.AddBypassRange(cidr); err != nil {
					t.Errorf("AddBypassRange(%s) error: %v", cidr, err)
				}
				if err := checker.RemoveBypassRange(cidr); err != nil {
					t.Errorf("RemoveBypassRange(%s) error: %v", cidr, err)
				}
			}
			done <- true
		}(i)
	}

	// Wait for all goroutines to complete
	for i := 0; i < 15; i++ {
		<-done
	}

	// Verify checker is still functional
	if !checker.ShouldBypass("10.0.0.1") {
		t.Error("Checker failed after concurrent access")
	}
}

func BenchmarkShouldBypass(b *testing.B) {
	checker, _ := NewBypassIPChecker([]string{
		"10.0.0.0/8",
		"172.16.0.0/12",
		"192.168.0.0/16",
		"2001:db8::/32",
	}, false)

	testIPs := []string{
		"10.0.0.1",
		"172.16.0.1",
		"192.168.1.1",
		"8.8.8.8",
		"2001:db8::1",
		"2001:db9::1",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ip := testIPs[i%len(testIPs)]
		checker.ShouldBypass(ip)
	}
}

func BenchmarkShouldBypassWithManyRanges(b *testing.B) {
	var ranges []string
	// Create 100 different ranges
	for i := 0; i < 100; i++ {
		ranges = append(ranges, fmt.Sprintf("10.%d.0.0/24", i))
	}

	checker, _ := NewBypassIPChecker(ranges, false)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		checker.ShouldBypass("10.50.0.1") // IP in middle of ranges
	}
}

func TestBypassIPCheckerImpl_Cache(t *testing.T) {
	tests := []struct {
		name         string
		bypassRanges []string
		testIP       string
		callCount    int
		wantBypass   bool
	}{
		{
			name:         "cache hit for repeated IP",
			bypassRanges: []string{"10.0.0.0/24"},
			testIP:       "10.0.0.100",
			callCount:    100,
			wantBypass:   true,
		},
		{
			name:         "cache hit for non-bypass IP",
			bypassRanges: []string{"10.0.0.0/24"},
			testIP:       "192.168.1.1",
			callCount:    100,
			wantBypass:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			checker, err := NewBypassIPChecker(tt.bypassRanges, false)
			if err != nil {
				t.Fatalf("Failed to create checker: %v", err)
			}

			// First call should check ranges
			result := checker.ShouldBypass(tt.testIP)
			if result != tt.wantBypass {
				t.Errorf("First call: ShouldBypass(%q) = %v, want %v", tt.testIP, result, tt.wantBypass)
			}

			// Subsequent calls should use cache
			for i := 0; i < tt.callCount-1; i++ {
				result := checker.ShouldBypass(tt.testIP)
				if result != tt.wantBypass {
					t.Errorf("Call %d: ShouldBypass(%q) = %v, want %v", i+2, tt.testIP, result, tt.wantBypass)
				}
			}
		})
	}
}

func TestBypassIPCheckerImpl_CacheControl(t *testing.T) {
	checker, err := NewBypassIPChecker([]string{"10.0.0.0/24"}, false)
	if err != nil {
		t.Fatalf("Failed to create checker: %v", err)
	}

	testIP := "10.0.0.100"

	// First call should populate cache
	if !checker.ShouldBypass(testIP) {
		t.Error("Expected bypass for IP in range")
	}

	// Disable cache
	checker.SetCacheEnabled(false)

	// Should still work without cache
	if !checker.ShouldBypass(testIP) {
		t.Error("Expected bypass for IP in range (cache disabled)")
	}

	// Re-enable cache
	checker.SetCacheEnabled(true)

	// Should work with cache again
	if !checker.ShouldBypass(testIP) {
		t.Error("Expected bypass for IP in range (cache re-enabled)")
	}

	// Clear cache
	checker.ClearCache()

	// Should still work after cache clear
	if !checker.ShouldBypass(testIP) {
		t.Error("Expected bypass for IP in range (after cache clear)")
	}
}

func TestBypassIPCheckerImpl_CacheInvalidation(t *testing.T) {
	checker, err := NewBypassIPChecker([]string{"10.0.0.0/24"}, false)
	if err != nil {
		t.Fatalf("Failed to create checker: %v", err)
	}

	testIP1 := "10.0.0.100"
	testIP2 := "192.168.1.100"

	// Cache result for IP1
	if !checker.ShouldBypass(testIP1) {
		t.Error("Expected bypass for IP1")
	}

	// Cache result for IP2
	if checker.ShouldBypass(testIP2) {
		t.Error("Expected no bypass for IP2")
	}

	// Add new range that includes IP2
	if err := checker.AddBypassRange("192.168.1.0/24"); err != nil {
		t.Fatalf("Failed to add bypass range: %v", err)
	}

	// Cache should be cleared, IP2 should now bypass
	if !checker.ShouldBypass(testIP2) {
		t.Error("Expected bypass for IP2 after adding range")
	}

	// Remove the original range
	if err := checker.RemoveBypassRange("10.0.0.0/24"); err != nil {
		t.Fatalf("Failed to remove bypass range: %v", err)
	}

	// Cache should be cleared, IP1 should no longer bypass
	if checker.ShouldBypass(testIP1) {
		t.Error("Expected no bypass for IP1 after removing range")
	}
}

func BenchmarkShouldBypassWithCache(b *testing.B) {
	checker, _ := NewBypassIPChecker([]string{
		"10.0.0.0/8",
		"172.16.0.0/12",
		"192.168.0.0/16",
	}, false)

	// Test with cache enabled (default)
	b.Run("WithCache", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			// Use same IP to test cache hit performance
			checker.ShouldBypass("10.0.0.1")
		}
	})

	// Test without cache
	b.Run("WithoutCache", func(b *testing.B) {
		checker.SetCacheEnabled(false)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			checker.ShouldBypass("10.0.0.1")
		}
	})
}

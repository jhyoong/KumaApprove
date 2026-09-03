package credstore

import "testing"

func TestGetMachineID(t *testing.T) {
	id, err := GetMachineID()
	if err != nil {
		t.Fatalf("failed to get machine ID: %v", err)
	}
	if len(id) == 0 {
		t.Fatal("machine ID is empty")
	}
	if len(id) < 16 {
		t.Fatalf("machine ID suspiciously short: %q", id)
	}

	id2, err := GetMachineID()
	if err != nil {
		t.Fatal(err)
	}
	if id != id2 {
		t.Fatal("machine ID not stable across calls")
	}
}

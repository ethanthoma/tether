package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
)

var deliveryProtocol = bendProtocol{"tether-bend-delivery-v1:", 96, "0123456"}

type deliveryState struct {
	confirmed int
	failed    bool
}

type deliveryPolicy struct {
	table      []byte
	state      deliveryState
	checked    int
	mismatches int
	active     bool
}

func readDeliveryTable(path string) ([]byte, error) {
	table, err := readBendTable(path, deliveryProtocol)
	if err != nil {
		return nil, err
	}
	for index := 0; index < len(table); index += 2 {
		if table[index] != '0' && table[index] != '1' {
			return nil, fmt.Errorf("delivery shadow: invalid state tag")
		}
	}
	return table, nil
}

func bendDeliveryStep(table []byte, allowed bool, state deliveryState, succeeded bool) (deliveryState, error) {
	if len(table) != deliveryProtocol.size || state.confirmed < 0 || state.confirmed > maxPushesPerDay {
		return deliveryState{}, fmt.Errorf("delivery shadow: invalid transition input")
	}
	flags := 0
	if allowed {
		flags |= 1
	}
	if state.failed {
		flags |= 2
	}
	if succeeded {
		flags |= 4
	}
	index := (state.confirmed*8 + flags) * 2
	return deliveryState{confirmed: int(table[index+1] - '0'), failed: table[index] == '1'}, nil
}

func openDeliveryPolicy(path, directory string) *deliveryPolicy {
	if path == "" {
		return &deliveryPolicy{}
	}
	table, err := readDeliveryTable(path)
	if err != nil {
		log.Printf("delivery shadow: unavailable for batch: %v", err)
		return &deliveryPolicy{}
	}
	observer := &deliveryPolicy{table: table}
	info, err := os.Lstat(filepath.Join(directory, "bend-delivery.enabled"))
	if os.IsNotExist(err) {
		return observer
	}
	if err != nil || !info.Mode().IsRegular() || info.Size() != 0 {
		log.Printf("nudge: invalid Bend delivery switch, using Go transitions")
		return observer
	}
	for confirmed := 0; confirmed <= maxPushesPerDay; confirmed++ {
		for flags := 0; flags < 8; flags++ {
			state := deliveryState{confirmed: confirmed, failed: flags&2 != 0}
			expected := state
			if flags&1 != 0 {
				expected = goDeliveryStep(state, flags&4 != 0)
			}
			actual, err := bendDeliveryStep(table, flags&1 != 0, state, flags&4 != 0)
			if err != nil || actual != expected {
				log.Printf("nudge: invalid Bend delivery semantics, using Go transitions")
				return observer
			}
		}
	}
	observer.active = true
	log.Printf("nudge: Bend delivery active")
	return observer
}

func goDeliveryStep(state deliveryState, succeeded bool) deliveryState {
	if !state.failed {
		if succeeded {
			state.confirmed++
		} else {
			state.failed = true
		}
	}
	return state
}

func (observer *deliveryPolicy) observe(succeeded bool) {
	expected := goDeliveryStep(observer.state, succeeded)
	if observer.table == nil {
		observer.state = expected
		return
	}
	actual, err := bendDeliveryStep(observer.table, true, observer.state, succeeded)
	observer.checked++
	if err != nil || actual != expected {
		observer.mismatches++
		observer.active = false
	}
	observer.state = expected
	if observer.active {
		observer.state = actual
	}
}

func (observer *deliveryPolicy) report() {
	if observer == nil || observer.table == nil {
		return
	}
	status := "ok"
	if observer.mismatches != 0 {
		status = "mismatch"
	}
	log.Printf(`{"status":%q,"policy":"bend-2.0.5/delivery-v1","source":"batch","transitions_checked":%d,"mismatches":%d,"confirmed":%d,"failed":%t}`,
		status, observer.checked, observer.mismatches, observer.state.confirmed, observer.state.failed)
}

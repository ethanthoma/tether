package main

import (
	"fmt"
	"log"
)

var deliveryProtocol = bendProtocol{"tether-bend-delivery-v1:", 96, "0123456"}

type deliveryState struct {
	confirmed int
	failed    bool
}

type deliveryShadow struct {
	table      []byte
	state      deliveryState
	checked    int
	mismatches int
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

func openDeliveryShadow(path string) *deliveryShadow {
	if path == "" {
		return nil
	}
	table, err := readDeliveryTable(path)
	if err != nil {
		log.Printf("delivery shadow: unavailable for batch: %v", err)
		return nil
	}
	return &deliveryShadow{table: table}
}

func (observer *deliveryShadow) observe(succeeded bool) {
	if observer == nil {
		return
	}
	expected := observer.state
	if !expected.failed {
		if succeeded {
			expected.confirmed++
		} else {
			expected.failed = true
		}
	}
	actual, err := bendDeliveryStep(observer.table, true, observer.state, succeeded)
	observer.checked++
	if err != nil || actual != expected {
		observer.mismatches++
	}
	observer.state = expected
}

func (observer *deliveryShadow) report() {
	if observer == nil {
		return
	}
	status := "ok"
	if observer.mismatches != 0 {
		status = "mismatch"
	}
	log.Printf(`{"status":%q,"policy":"bend-2.0.5/delivery-v1","source":"batch","transitions_checked":%d,"mismatches":%d,"confirmed":%d,"failed":%t}`,
		status, observer.checked, observer.mismatches, observer.state.confirmed, observer.state.failed)
}

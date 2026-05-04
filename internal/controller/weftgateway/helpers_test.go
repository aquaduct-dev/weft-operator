/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package weftgateway

import (
	"fmt"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	weftv1alpha1 "aquaduct.dev/weft-operator/api/v1alpha1"
)

func hostPtr(s string) *gatewayv1.Hostname {
	h := gatewayv1.Hostname(s)
	return &h
}

func TestClassifyListenerHostnames_L7Only(t *testing.T) {
	gw := &gatewayv1.Gateway{
		Spec: gatewayv1.GatewaySpec{
			Listeners: []gatewayv1.Listener{
				{Protocol: gatewayv1.HTTPProtocolType, Hostname: hostPtr("a.example.com")},
				{Protocol: gatewayv1.HTTPSProtocolType, Hostname: hostPtr("b.example.com")},
				{Protocol: gatewayv1.TLSProtocolType, Hostname: hostPtr("c.example.com")},
			},
		},
	}
	specs := classifyListenerHostnames(gw)
	if len(specs) != 3 {
		t.Fatalf("expected 3 specs, got %d", len(specs))
	}
	for _, s := range specs {
		if s.L4 {
			t.Errorf("hostname %q should be L7 (HTTP/HTTPS/TLS demux on bastion)", s.Hostname)
		}
	}
}

func TestClassifyListenerHostnames_TCPMakesL4(t *testing.T) {
	gw := &gatewayv1.Gateway{
		Spec: gatewayv1.GatewaySpec{
			Listeners: []gatewayv1.Listener{
				{Protocol: gatewayv1.TCPProtocolType, Hostname: hostPtr("ssh.example.com")},
			},
		},
	}
	specs := classifyListenerHostnames(gw)
	if len(specs) != 1 || !specs[0].L4 {
		t.Errorf("TCP listener should yield L4, got %+v", specs)
	}
}

func TestClassifyListenerHostnames_UDPMakesL4(t *testing.T) {
	gw := &gatewayv1.Gateway{
		Spec: gatewayv1.GatewaySpec{
			Listeners: []gatewayv1.Listener{
				{Protocol: gatewayv1.UDPProtocolType, Hostname: hostPtr("dns.example.com")},
			},
		},
	}
	specs := classifyListenerHostnames(gw)
	if len(specs) != 1 || !specs[0].L4 {
		t.Errorf("UDP listener should yield L4, got %+v", specs)
	}
}

func TestClassifyListenerHostnames_MixedProtocolsYieldL4(t *testing.T) {
	// A hostname with both HTTPS:443 and TCP:5432 cannot fan to all
	// bastions: the HTTPS half could but the TCP half would collide.
	// "Any L4 listener wins" is the safe pessimistic rule.
	gw := &gatewayv1.Gateway{
		Spec: gatewayv1.GatewaySpec{
			Listeners: []gatewayv1.Listener{
				{Protocol: gatewayv1.HTTPSProtocolType, Hostname: hostPtr("api.example.com")},
				{Protocol: gatewayv1.TCPProtocolType, Hostname: hostPtr("api.example.com")},
			},
		},
	}
	specs := classifyListenerHostnames(gw)
	if len(specs) != 1 {
		t.Fatalf("expected 1 deduplicated spec, got %+v", specs)
	}
	if !specs[0].L4 {
		t.Errorf("mixed-protocol hostname must be L4 (TCP listener can't share IP+port)")
	}
}

func TestClassifyListenerHostnames_PreservesEncounterOrder(t *testing.T) {
	gw := &gatewayv1.Gateway{
		Spec: gatewayv1.GatewaySpec{
			Listeners: []gatewayv1.Listener{
				{Protocol: gatewayv1.HTTPProtocolType, Hostname: hostPtr("z.example.com")},
				{Protocol: gatewayv1.HTTPProtocolType, Hostname: hostPtr("a.example.com")},
				{Protocol: gatewayv1.TCPProtocolType, Hostname: hostPtr("z.example.com")},
			},
		},
	}
	specs := classifyListenerHostnames(gw)
	if len(specs) != 2 || specs[0].Hostname != "z.example.com" || specs[1].Hostname != "a.example.com" {
		t.Fatalf("expected [z,a] in encounter order, got %+v", specs)
	}
	if !specs[0].L4 {
		t.Errorf("z.example.com should be L4 due to its TCP listener")
	}
	if specs[1].L4 {
		t.Errorf("a.example.com should be L7 (only HTTP listener)")
	}
}

func TestClassifyListenerHostnames_SkipsEmptyHostnames(t *testing.T) {
	empty := gatewayv1.Hostname("")
	gw := &gatewayv1.Gateway{
		Spec: gatewayv1.GatewaySpec{
			Listeners: []gatewayv1.Listener{
				{Protocol: gatewayv1.TCPProtocolType, Hostname: nil},
				{Protocol: gatewayv1.TCPProtocolType, Hostname: &empty},
			},
		},
	}
	specs := classifyListenerHostnames(gw)
	if len(specs) != 0 {
		t.Errorf("expected 0 specs for nil/empty hostnames, got %+v", specs)
	}
}

func TestPickL4Bastion_DeterministicAcrossCalls(t *testing.T) {
	bastions := []weftv1alpha1.BastionInfo{
		{ID: "b1", IP: "1.1.1.1"},
		{ID: "b2", IP: "2.2.2.2"},
		{ID: "b3", IP: "3.3.3.3"},
	}
	gw := &gatewayv1.Gateway{ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "gw"}}
	a := pickL4Bastion(bastions, gw, "ssh.example.com")
	b := pickL4Bastion(bastions, gw, "ssh.example.com")
	if a == "" || a != b {
		t.Errorf("expected stable non-empty pick; got %q vs %q", a, b)
	}
}

func TestPickL4Bastion_StableUnderInputOrder(t *testing.T) {
	// Bastions arriving in different orders from the TaaS list should
	// not change the chosen bastion — eligibleBastionIDs sorts before
	// hashing, so the pick is order-independent.
	gw := &gatewayv1.Gateway{ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "gw"}}
	asc := []weftv1alpha1.BastionInfo{
		{ID: "b1", IP: "1.1.1.1"},
		{ID: "b2", IP: "2.2.2.2"},
		{ID: "b3", IP: "3.3.3.3"},
	}
	desc := []weftv1alpha1.BastionInfo{
		{ID: "b3", IP: "3.3.3.3"},
		{ID: "b2", IP: "2.2.2.2"},
		{ID: "b1", IP: "1.1.1.1"},
	}
	if pickL4Bastion(asc, gw, "ssh.example.com") != pickL4Bastion(desc, gw, "ssh.example.com") {
		t.Errorf("pick should be order-independent")
	}
}

func TestPickL4Bastion_SkipsSuspendedAndNoIP(t *testing.T) {
	bastions := []weftv1alpha1.BastionInfo{
		{ID: "b-suspended", IP: "1.1.1.1", Suspended: true},
		{ID: "b-noip"},
		{ID: "b-good", IP: "9.9.9.9"},
	}
	gw := &gatewayv1.Gateway{ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "gw"}}
	got := pickL4Bastion(bastions, gw, "ssh.example.com")
	if got != "b-good" {
		t.Errorf("expected the only eligible bastion; got %q", got)
	}
}

func TestPickL4Bastion_EmptyOnNoEligible(t *testing.T) {
	bastions := []weftv1alpha1.BastionInfo{
		{ID: "b-suspended", IP: "1.1.1.1", Suspended: true},
		{ID: "b-noip"},
	}
	gw := &gatewayv1.Gateway{ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "gw"}}
	if got := pickL4Bastion(bastions, gw, "ssh.example.com"); got != "" {
		t.Errorf("expected empty on no eligible bastion, got %q", got)
	}
}

func TestPickL4Bastion_SpreadsAcrossFleet(t *testing.T) {
	// Different hostnames should land on different bastions often
	// enough that one hot bastion isn't carrying every L4 tunnel.
	bastions := []weftv1alpha1.BastionInfo{
		{ID: "b1", IP: "1.1.1.1"},
		{ID: "b2", IP: "2.2.2.2"},
		{ID: "b3", IP: "3.3.3.3"},
	}
	gw := &gatewayv1.Gateway{ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "gw"}}
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		seen[pickL4Bastion(bastions, gw, fmt.Sprintf("h-%d.example.com", i))] = true
	}
	if len(seen) < 2 {
		t.Errorf("expected hash to spread across multiple bastions, hit %d", len(seen))
	}
}

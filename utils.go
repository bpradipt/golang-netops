package main

import (
	"fmt"
	"math"
	"net/netip"
	"path/filepath"
	"strings"

	"github.com/bpradipt/golang-netops/netops"
)

func findPrimaryInterface(ns netops.Namespace) (string, error) {

	routes, err := ns.RouteList(&netops.Route{Destination: netops.DefaultPrefix})
	if err != nil {
		return "", fmt.Errorf("failed to get routes on namespace %q: %w", ns.Path(), err)
	}

	var priority = math.MaxInt
	var dev string

	for _, r := range routes {
		if r.Destination.Bits() == 0 && r.Priority < priority {
			dev = r.Device
		}
		fmt.Printf("Default route (%v) found on device %q\n", r, dev)
	}

	if dev == "" {
		return "", fmt.Errorf("failed to identify destination interface of default gateway on network namespace %q", ns.Path())
	}

	// The first device discovered with default route is considered the primary interface.
	return dev, nil
}

func getInterfaceDetails(ns netops.Namespace, iface string) (
	addrCIDR netip.Prefix, defRoute *netops.Route, err error) {

	link, err := ns.LinkFind(iface)
	if err != nil {
		return netip.Prefix{}, nil, fmt.Errorf("failed to find link %q: %w", iface, err)
	}

	addrs, err := link.GetAddr()
	if err != nil {
		return netip.Prefix{}, nil, fmt.Errorf("failed to get addresses for link %q: %w", link.Name(), err)
	}

	for _, a := range addrs {
		if a.Addr().Is4() {
			addrCIDR = a
			break
		}
	}

	routes, err := ns.RouteList(&netops.Route{Destination: netops.DefaultPrefix})
	if err != nil {
		return netip.Prefix{}, nil, fmt.Errorf("failed to get routes on namespace %q: %w", ns.Path(), err)
	}

	for _, r := range routes {
		if r.Device == iface && r.Destination.Bits() == 0 && r.Priority < math.MaxInt {
			defRoute = r
			break
		}
	}

	fmt.Printf("Interface %q IPv4 address %q and default route %v\n", iface, addrCIDR, defRoute)

	return addrCIDR, defRoute, nil
}

func getSecondaryInterfaceDetails(ns netops.Namespace, primaryInterface string) (
	secIface string, secAddrCIDR netip.Prefix, secRoute *netops.Route, err error) {

	links, err := ns.LinkList()
	if err != nil {
		return "", netip.Prefix{}, nil, fmt.Errorf("failed to list links in namespace %q: %w", ns.Path(), err)
	}

	for _, link := range links {
		if isInterfaceFilteredOut(link.Name()) || link.Name() == primaryInterface {
			continue
		}

		addr, route, err := getInterfaceDetails(ns, link.Name())
		if err != nil {
			return "", netip.Prefix{}, nil, err
		}

		// The first interface other than the primary having an IPv4 address and a default route
		// or an IPbv4 address in the same subnet as the primary interface with no default route
		// is considered the secondary interface.
		if addr.IsValid() && route != nil {
			secIface = link.Name()
			secAddrCIDR = addr
			secRoute = route
			fmt.Printf("Secondary interface %q found with IPv4 address %q\n", secIface, secAddrCIDR)
			break
		}

		// If route == nil, then check if the addr is in the same subnet as the primary interface
		// Use default route for the primary as the route for the secondary
		if addr.IsValid() && route == nil {
			fmt.Printf("Route is nil, checking if %q is in the same subnet as %q\n", addr, primaryInterface)
			priAddrCIDR, defRoute, err := getInterfaceDetails(ns, primaryInterface)
			if err != nil {
				return "", netip.Prefix{}, nil, err
			}
			if priAddrCIDR.Bits() == addr.Bits() {
				secIface = link.Name()
				secAddrCIDR = addr
				secRoute = &netops.Route{
					Destination: defRoute.Destination,
					Gateway:     defRoute.Gateway,
					Device:      secIface,
					// Change the Route.Device to the secondary interface
				}
				fmt.Printf("Secondary interface %q found with IPv4 address %q\n", secIface, secAddrCIDR)
				break
			}
		}
	}

	return secIface, secAddrCIDR, secRoute, nil
}

// isInterfaceFilteredOut filters out interface names that begin with certain prefixes.
// This is used to ignore virtual, loopback, and other non-relevant interfaces.
//
// The prefixes to filter out are:
// "veth", "lo", "docker", "podman", "br-", "cni", "tunl", "tun", "tap"
var allowedPrefixes = []string{
	"veth", "lo", "docker", "podman", "br-", "cni", "tunl", "tun", "tap",
}

func isInterfaceFilteredOut(ifName string) bool {

	for _, prefix := range allowedPrefixes {
		if strings.HasPrefix(ifName, prefix) {
			return true
		}
	}

	return false
}

// Function to move the network interface to a different network namespace
// Also set the default route and address for the interface in the new namespace

func moveInterfaceToNamespace(srcNs netops.Namespace, destNsName string, iface string, addrCIDR netip.Prefix, route *netops.Route) error {

	nsPath, err := netops.CreateNamedNamespace(destNsName)
	// Check if the namespace already exists
	if err != nil {
		if strings.Contains(err.Error(), "file exists") {
			nsPath = filepath.Join("/run/netns", destNsName)
		} else {
			return fmt.Errorf("failed to create namespace %q: %w", destNsName, err)
		}
	}

	ns, err := netops.OpenNamespace(nsPath)
	if err != nil {
		return fmt.Errorf("failed to open namespace %q: %w", ns, err)
	}

	// Get the network interface object
	link, err := srcNs.LinkFind(iface)
	if err != nil {
		return fmt.Errorf("failed to get link %q: %w", iface, err)
	}

	// Move the network interface to the new namespace
	err = link.SetNamespace(ns)
	if err != nil {
		return fmt.Errorf("failed to move link %q to namespace %q: %w", iface, ns.Path(), err)
	}

	// Set the address for the network interface in the new namespace
	err = link.AddAddr(addrCIDR)
	if err != nil {
		return fmt.Errorf("failed to set address %q for link %q: %w", addrCIDR, iface, err)
	}

	// Bring up the network interface in the new namespace
	err = link.SetUp()
	if err != nil {
		return fmt.Errorf("failed to bring up link %q: %w", iface, err)
	}

	// Set the default route for the network interface in the new namespace
	err = ns.RouteAdd(route)
	if err != nil {
		return fmt.Errorf("failed to set route %v for link %q: %w", route, iface, err)
	}

	return nil
}

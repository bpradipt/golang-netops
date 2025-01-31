package main

import (
	"fmt"
	"log"

	//"github.com/confidential-containers/cloud-api-adaptor/src/cloud-api-adaptor/pkg/util/netops"
	"github.com/bpradipt/golang-netops/netops"
)

func main() {
	// Open the current network namespace (host namespace)
	ns, err := netops.OpenCurrentNamespace()
	if err != nil {
		log.Fatalf("Error opening current namespace: %v", err)
	}
	defer ns.Close()

	// List all links (network interfaces)
	links, err := ns.LinkList()
	if err != nil {
		log.Fatalf("Error listing links: %v", err)
	}

	fmt.Println("Network Interfaces and their Addresses:")
	for _, link := range links {
		fmt.Printf("Interface: %s\n", link.Name())
		addresses, err := link.GetAddr()
		if err != nil {
			log.Printf("Error getting addresses for interface %s: %v", link.Name(), err)
			continue
		}
		for _, addr := range addresses {
			fmt.Printf("  Address: %s\n", addr)
		}
	}

	// List all routes
	routes, err := ns.RouteList()
	if err != nil {
		log.Fatalf("Error listing routes: %v", err)
	}

	fmt.Println("\nRoutes:")
	for _, route := range routes {
		fmt.Printf("Destination: %s, Gateway: %s, Device: %s\n", route.Destination, route.Gateway, route.Device)
	}

	priIface, err := findPrimaryInterface(ns)

	if err != nil {
		log.Fatalf("Error finding primary interface: %v", err)
	}

	fmt.Printf("Primary interface in %s: %s\n", ns.Path(), priIface)

	secIface, secAddrCIDR, secRoute, err := getSecondaryInterfaceDetails(ns, priIface)
	if err != nil {
		log.Fatalf("Error finding secondary interface: %v", err)
	}

	if secIface == "" {
		log.Fatalf("No secondary interface found")
	}

	fmt.Printf("Secondary interface in %s: %s\n", ns.Path(), secIface)

	// Move sface to a new network namespace "podns"
	err = moveInterfaceToNamespace(ns, "podns", secIface, secAddrCIDR, secRoute)
	if err != nil {
		log.Fatalf("Error moving secondary interface to namespace: %v", err)
	}
	defer netops.DeleteNamedNamespace("podns")

	fmt.Printf("Successfully moved secondary interface %s to namespace podns\n", secIface)

	// Get interface details for sface in the new namespace "podns"

	podns, err := netops.OpenNamespaceByName("podns")
	if err != nil {
		log.Fatalf("Error opening current namespace: %v", err)
	}
	defer podns.Close()

	addrCIDR, defRoute, err := getInterfaceDetails(podns, secIface)
	if err != nil {
		log.Fatalf("Error getting interface details in namespace podns: %v", err)
	}

	fmt.Printf("Interface %q IPv4 address %q and default route %v\n", secIface, addrCIDR, defRoute)

}

/*
 *
 * Copyright (c) 2020 vesoft inc. All rights reserved.
 *
 * This source code is licensed under Apache 2.0 License.
 *
 */

package nebula_go

import (
	"fmt"
	"net"
	"os"
)

type HostAddress struct {
	Host string
	Port int
}

func DomainToIP(addresses []HostAddress) ([]HostAddress, error) {
	var newHostsList []HostAddress
	for _, host := range addresses {
		// Get ip from domain
		ips, err := net.LookupIP(host.Host)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Could not get IPs: %v\n", err)
			return nil, err
		}
		convHost := HostAddress{Host: ips[0].String(), Port: host.Port}
		newHostsList = append(newHostsList, convHost)
	}
	return newHostsList, nil
}

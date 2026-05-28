package serverstats

import (
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"strings"
)

var carrierGradeNAT = netip.MustParsePrefix("100.64.0.0/10")

// Endpoint is a validated public Source query endpoint.
type Endpoint struct {
	Address string
	IP      netip.Addr
	Port    uint16
}

// ParseEndpoint accepts only public literal IP:port endpoints.
func ParseEndpoint(value string) (Endpoint, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return Endpoint{}, fmt.Errorf("address is required; use a public IP and query port such as 203.0.113.10:27015")
	}

	host, portText, err := net.SplitHostPort(value)
	if err != nil {
		return Endpoint{}, fmt.Errorf("address must be a public IP and query port such as 203.0.113.10:27015 or [2001:db8::1]:27015")
	}

	ip, err := netip.ParseAddr(host)
	if err != nil {
		return Endpoint{}, fmt.Errorf("address host must be a literal public IP; hostnames are not supported")
	}
	ip = ip.Unmap()

	port, err := strconv.ParseUint(portText, 10, 16)
	if err != nil || port == 0 {
		return Endpoint{}, fmt.Errorf("address must include a query port from 1 to 65535")
	}

	if !isAllowedPublicIP(ip) {
		return Endpoint{}, fmt.Errorf("address must target a public internet IP; local and private destinations are not allowed")
	}

	portNumber := uint16(port)
	return Endpoint{
		Address: net.JoinHostPort(ip.String(), strconv.Itoa(int(portNumber))),
		IP:      ip,
		Port:    portNumber,
	}, nil
}

func isAllowedPublicIP(ip netip.Addr) bool {
	if !ip.IsValid() ||
		!ip.IsGlobalUnicast() ||
		ip.IsUnspecified() ||
		ip.IsLoopback() ||
		ip.IsPrivate() ||
		ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() ||
		ip.IsMulticast() {
		return false
	}

	if ip.Is4() && carrierGradeNAT.Contains(ip) {
		return false
	}

	return true
}

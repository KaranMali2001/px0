package main

import (
	"fmt"
	"net"
	"net/url"
	"path/filepath"
	"sort"
	"strconv"
)

func viewerURL(addr, initialFile string, initialLine int, basePath ...string) string {
	u := url.URL{Scheme: "http", Host: addr}
	bp := "/"
	if len(basePath) > 0 && basePath[0] != "" {
		bp = cleanBasePath(basePath[0])
	}
	if bp != "/" {
		u.Path = bp
	}
	q := u.Query()
	if initialFile != "" {
		q.Set("path", filepath.ToSlash(initialFile))
	}
	if initialLine > 0 {
		q.Set("line", strconv.Itoa(initialLine))
	}
	if len(q) > 0 {
		u.RawQuery = q.Encode()
	}
	return u.String()
}

// networkURLs returns URLs for the machine's non-loopback IPv4 addresses.
// The supplied address contributes the bound port; its host is intentionally
// ignored because a bind address is not necessarily a usable destination.
func networkURLs(addr, initialFile string, initialLine int) []string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil
	}
	return networkURLsFromAddrs(addr, initialFile, initialLine, addrs)
}

func networkURLsFromAddrs(addr, initialFile string, initialLine int, addrs []net.Addr) []string {
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil
	}

	seen := make(map[string]struct{})
	urls := make([]string, 0, len(addrs))
	for _, addr := range addrs {
		var ip net.IP
		switch a := addr.(type) {
		case *net.IPNet:
			ip = a.IP
		case *net.IPAddr:
			ip = a.IP
		default:
			continue
		}

		ip = ip.To4()
		if ip == nil || ip.IsLoopback() || ip.IsUnspecified() {
			continue
		}

		host := ip.String()
		if _, ok := seen[host]; ok {
			continue
		}
		seen[host] = struct{}{}
		urls = append(urls, viewerURL(net.JoinHostPort(host, port), initialFile, initialLine))
	}

	sort.Strings(urls)
	return urls
}

// listen binds the requested port, walking forward if it is already taken so a
// second instance does not simply fail. If a block of ports is busy, it falls
// back to an OS-assigned free port.
func listen(host string, port int) (net.Listener, string, error) {
	if port == 0 {
		ln, err := net.Listen("tcp", net.JoinHostPort(host, "0"))
		if err != nil {
			return nil, "", err
		}
		return ln, ln.Addr().String(), nil
	}
	for p := port; p < port+100; p++ {
		addr := net.JoinHostPort(host, fmt.Sprint(p))
		if ln, err := net.Listen("tcp", addr); err == nil {
			return ln, addr, nil
		}
	}
	// Fallback to any free port assigned by the OS if port range is busy
	if ln, err := net.Listen("tcp", net.JoinHostPort(host, "0")); err == nil {
		return ln, ln.Addr().String(), nil
	}
	return nil, "", fmt.Errorf("no free port available starting from %d", port)
}

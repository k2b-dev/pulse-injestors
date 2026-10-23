package uptime

// DNSBL needs to distinguish NXDOMAIN (not listed) from an empty NOERROR
// response (check unavailable). net.Resolver folds both into IsNotFound, so
// these checks use the DNS wire protocol with UDP and TCP truncation fallback.
import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"time"
)

var dnsTypes = map[string]uint16{"A": 1, "NS": 2, "CNAME": 5, "MX": 15, "TXT": 16, "AAAA": 28}

func lookupRecords(ctx context.Context, resolver, name, typ string) ([]string, bool, error) {
	qt, ok := dnsTypes[typ]
	if !ok {
		return nil, false, errors.New("unsupported DNS record type")
	}
	servers := []string{resolver}
	if resolver == "" {
		data, err := os.ReadFile("/etc/resolv.conf")
		if err != nil {
			return nil, false, err
		}
		servers = nil
		for _, line := range strings.Split(string(data), "\n") {
			f := strings.Fields(line)
			if len(f) > 1 && f[0] == "nameserver" && net.ParseIP(f[1]) != nil {
				servers = append(servers, net.JoinHostPort(f[1], "53"))
			}
		}
		if len(servers) == 0 {
			return nil, false, errors.New("no system DNS resolver configured")
		}
	}
	query := make([]byte, 12)
	if _, err := rand.Read(query[:2]); err != nil {
		return nil, false, err
	}
	binary.BigEndian.PutUint16(query[2:4], 0x0100)
	binary.BigEndian.PutUint16(query[4:6], 1)
	name = strings.ToLower(strings.TrimSuffix(name, "."))
	if len(name) == 0 || len(name) > 253 {
		return nil, false, errors.New("invalid DNS name")
	}
	for _, label := range strings.Split(name, ".") {
		if len(label) == 0 || len(label) > 63 {
			return nil, false, errors.New("invalid DNS label")
		}
		query = append(query, byte(len(label)))
		query = append(query, label...)
	}
	query = append(query, 0, byte(qt>>8), byte(qt), 0, 1)
	var lastErr error
	for _, server := range servers {
		response, err := dnsExchange(ctx, server, "udp", query)
		if err != nil {
			lastErr = err
			continue
		}
		if len(response) >= 4 && binary.BigEndian.Uint16(response[2:4])&0x0200 != 0 {
			response, err = dnsExchange(ctx, server, "tcp", query)
			if err != nil {
				lastErr = err
				continue
			}
		}
		values, absent, err := parseDNSResponse(response, query, name, qt)
		if err == nil {
			return values, absent, nil
		}
		lastErr = err
	}
	return nil, false, lastErr
}
func dnsExchange(ctx context.Context, server, network string, query []byte) ([]byte, error) {
	conn, err := (&net.Dialer{}).DialContext(ctx, network, server)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	deadline := time.Now().Add(2 * time.Second)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	if err := conn.SetDeadline(deadline); err != nil {
		return nil, err
	}
	if network == "tcp" {
		framed := make([]byte, 2, len(query)+2)
		binary.BigEndian.PutUint16(framed, uint16(len(query)))
		framed = append(framed, query...)
		if _, err := conn.Write(framed); err != nil {
			return nil, err
		}
		var size [2]byte
		if _, err := io.ReadFull(conn, size[:]); err != nil {
			return nil, err
		}
		body := make([]byte, int(binary.BigEndian.Uint16(size[:])))
		_, err = io.ReadFull(conn, body)
		return body, err
	}
	if _, err := conn.Write(query); err != nil {
		return nil, err
	}
	body := make([]byte, 65535)
	n, err := conn.Read(body)
	return body[:n], err
}

func dnsName(packet []byte, start int) (string, int, error) {
	pos := start
	next := -1
	var labels []string
	length := 0
	for steps := 0; steps < len(packet); steps++ {
		if pos >= len(packet) {
			break
		}
		size := int(packet[pos])
		pos++
		if size == 0 {
			if next < 0 {
				next = pos
			}
			return strings.ToLower(strings.Join(labels, ".")), next, nil
		}
		if size&0xc0 == 0xc0 {
			if pos >= len(packet) {
				break
			}
			if next < 0 {
				next = pos + 1
			}
			pos = (size&0x3f)<<8 | int(packet[pos])
			continue
		}
		if size > 63 || pos+size > len(packet) {
			break
		}
		length += size + 1
		if length > 254 {
			break
		}
		labels = append(labels, string(packet[pos:pos+size]))
		pos += size
	}
	return "", 0, errors.New("malformed DNS name")
}
func parseDNSResponse(packet, query []byte, name string, qt uint16) ([]string, bool, error) {
	bad := errors.New("malformed DNS response")
	if len(packet) < 12 || packet[0] != query[0] || packet[1] != query[1] {
		return nil, false, bad
	}
	flags := binary.BigEndian.Uint16(packet[2:4])
	if flags&0x8000 == 0 || flags&0x7800 != 0 || flags&0x0200 != 0 || binary.BigEndian.Uint16(packet[4:6]) != 1 {
		return nil, false, bad
	}
	qname, pos, err := dnsName(packet, 12)
	if err != nil || pos+4 > len(packet) || qname != name || binary.BigEndian.Uint16(packet[pos:pos+2]) != qt || binary.BigEndian.Uint16(packet[pos+2:pos+4]) != 1 {
		return nil, false, bad
	}
	pos += 4
	switch flags & 15 {
	case 3:
		return nil, true, nil
	case 0:
	default:
		return nil, false, fmt.Errorf("DNS response code %d", flags&15)
	}
	type answer struct {
		owner string
		typ   uint16
		value string
	}
	var answers []answer
	count := int(binary.BigEndian.Uint16(packet[6:8]))
	for i := 0; i < count; i++ {
		owner, next, err := dnsName(packet, pos)
		if err != nil || next+10 > len(packet) {
			return nil, false, bad
		}
		pos = next
		typ := binary.BigEndian.Uint16(packet[pos : pos+2])
		class := binary.BigEndian.Uint16(packet[pos+2 : pos+4])
		size := int(binary.BigEndian.Uint16(packet[pos+8 : pos+10]))
		pos += 10
		end := pos + size
		if end > len(packet) {
			return nil, false, bad
		}
		value := ""
		if class == 1 {
			switch typ {
			case 1:
				if size != 4 {
					return nil, false, bad
				}
				value = net.IP(packet[pos:end]).String()
			case 28:
				if size != 16 {
					return nil, false, bad
				}
				value = net.IP(packet[pos:end]).String()
			case 2, 5:
				value, next, err = dnsName(packet, pos)
				if err != nil || next != end {
					return nil, false, bad
				}
			case 15:
				if size < 3 {
					return nil, false, bad
				}
				var host string
				host, next, err = dnsName(packet, pos+2)
				if err != nil || next != end {
					return nil, false, bad
				}
				value = fmt.Sprintf("%d %s", binary.BigEndian.Uint16(packet[pos:pos+2]), host)
			case 16:
				for p := pos; p < end; {
					n := int(packet[p])
					p++
					if p+n > end {
						return nil, false, bad
					}
					value += string(packet[p : p+n])
					p += n
				}
			}
			answers = append(answers, answer{owner, typ, value})
		}
		pos = end
	}
	// Accept records only for the queried owner or its returned CNAME chain.
	owners := map[string]bool{name: true}
	for i := 0; i < len(answers); i++ {
		for _, a := range answers {
			if qt != 5 && a.typ == 5 && owners[a.owner] {
				owners[a.value] = true
			}
		}
	}
	var values []string
	for _, a := range answers {
		if a.typ == qt && owners[a.owner] {
			values = append(values, a.value)
		}
	}
	return values, false, nil
}

package uptime

import (
	"context"
	"encoding/binary"
	"io"
	"net"
	"sync"
	"testing"
	"time"
)

func TestDNSTruncationRetriesTCP(t *testing.T) {
	tcp, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	udp, err := net.ListenPacket("udp", tcp.Addr().String())
	if err != nil {
		tcp.Close()
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		buf := make([]byte, 4096)
		n, addr, err := udp.ReadFrom(buf)
		if err != nil {
			return
		}
		response := append([]byte(nil), buf[:n]...)
		binary.BigEndian.PutUint16(response[2:4], 0x8380)
		_, _ = udp.WriteTo(response, addr)
	}()
	go func() {
		defer wg.Done()
		conn, err := tcp.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
		var size [2]byte
		if _, err := io.ReadFull(conn, size[:]); err != nil {
			return
		}
		query := make([]byte, int(binary.BigEndian.Uint16(size[:])))
		if _, err := io.ReadFull(conn, query); err != nil {
			return
		}
		response := append([]byte(nil), query...)
		binary.BigEndian.PutUint16(response[2:4], 0x8180)
		binary.BigEndian.PutUint16(response[6:8], 1)
		response = append(response, 0xc0, 0x0c, 0, 1, 0, 1, 0, 0, 0, 60, 0, 4, 192, 0, 2, 1)
		binary.BigEndian.PutUint16(size[:], uint16(len(response)))
		_, _ = conn.Write(append(size[:], response...))
	}()
	defer func() { tcp.Close(); udp.Close(); wg.Wait() }()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	values, absent, err := lookupRecords(ctx, tcp.Addr().String(), "example.test", "A")
	if err != nil || absent || len(values) != 1 || values[0] != "192.0.2.1" {
		t.Fatalf("%v %v %v", values, absent, err)
	}
}
func TestDNSParserRejectsMalformedAndUnrelatedAnswers(t *testing.T) {
	query := make([]byte, 12)
	query[0] = 42
	binary.BigEndian.PutUint16(query[4:6], 1)
	query = append(query, wireName("example.test")...)
	query = append(query, 0, 1, 0, 1)
	response := append([]byte(nil), query...)
	binary.BigEndian.PutUint16(response[2:4], 0x8180)
	binary.BigEndian.PutUint16(response[6:8], 1)
	response = append(response, 0xc0, 0x0c, 0, 1, 0, 1, 0, 0, 0, 60, 0, 4, 192, 0, 2, 1)
	for i := 0; i < len(response); i++ {
		if _, _, err := parseDNSResponse(response[:i], query, "example.test", 1); err == nil {
			t.Fatalf("accepted truncated response at %d", i)
		}
	}
	for _, mutate := range []func([]byte){func(b []byte) { b[0]++ }, func(b []byte) { b[2] = 1 }, func(b []byte) { b[12] = 0xc0; b[13] = 12 }, func(b []byte) { b[4] = 1 }} {
		b := append([]byte(nil), response...)
		mutate(b)
		if _, _, err := parseDNSResponse(b, query, "example.test", 1); err == nil {
			t.Fatal("accepted malformed response")
		}
	}
	unrelated := append([]byte(nil), query...)
	binary.BigEndian.PutUint16(unrelated[2:4], 0x8180)
	binary.BigEndian.PutUint16(unrelated[6:8], 1)
	unrelated = append(unrelated, wireName("other.test")...)
	unrelated = append(unrelated, 0, 1, 0, 1, 0, 0, 0, 60, 0, 4, 192, 0, 2, 1)
	values, absent, err := parseDNSResponse(unrelated, query, "example.test", 1)
	if err != nil || absent || len(values) != 0 {
		t.Fatalf("unrelated answer accepted: %v %v %v", values, absent, err)
	}
}

func TestDNSCNAMEIsTheQueriedRecordNotEveryAlias(t *testing.T) {
	query := make([]byte, 12)
	query[0] = 42
	binary.BigEndian.PutUint16(query[4:6], 1)
	query = append(query, wireName("example.test")...)
	query = append(query, 0, 5, 0, 1)
	response := append([]byte(nil), query...)
	binary.BigEndian.PutUint16(response[2:4], 0x8180)
	binary.BigEndian.PutUint16(response[6:8], 2)
	for _, names := range [][2]string{{"example.test", "alias.test"}, {"alias.test", "final.test"}} {
		response = append(response, wireName(names[0])...)
		data := wireName(names[1])
		response = append(response, 0, 5, 0, 1, 0, 0, 0, 60, 0, byte(len(data)))
		response = append(response, data...)
	}
	values, absent, err := parseDNSResponse(response, query, "example.test", 5)
	if err != nil || absent || len(values) != 1 || values[0] != "alias.test" {
		t.Fatalf("%v %v %v", values, absent, err)
	}
}

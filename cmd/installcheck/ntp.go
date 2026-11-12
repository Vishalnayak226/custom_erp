package main

import (
	"encoding/binary"
	"net"
	"time"
)

// sntpOffset asks a public time server how far this clock is off (a plain
// SNTP v4 client request; nothing is sent but the 48-byte request).
func sntpOffset() (time.Duration, error) {
	var last error
	for _, host := range []string{"pool.ntp.org:123", "time.cloudflare.com:123"} {
		off, err := sntpQuery(host)
		if err == nil {
			return off, nil
		}
		last = err
	}
	return 0, last
}

func sntpQuery(addr string) (time.Duration, error) {
	conn, err := net.DialTimeout("udp", addr, 3*time.Second)
	if err != nil {
		return 0, err
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(3 * time.Second))
	req := make([]byte, 48)
	req[0] = 0x23 // LI 0, version 4, mode 3 (client)
	t0 := time.Now()
	if _, err := conn.Write(req); err != nil {
		return 0, err
	}
	resp := make([]byte, 48)
	if _, err := conn.Read(resp); err != nil {
		return 0, err
	}
	return sntpFrom(resp, t0, time.Now()), nil
}

// sntpFrom reads the server's transmit time out of a reply and returns the
// offset of the local clock (positive: the local clock is ahead).
func sntpFrom(resp []byte, t0, t3 time.Time) time.Duration {
	secs := binary.BigEndian.Uint32(resp[40:44])
	frac := binary.BigEndian.Uint32(resp[44:48])
	const ntpToUnix = 2208988800
	server := time.Unix(int64(secs)-ntpToUnix, int64(uint64(frac)*1e9>>32))
	mid := t0.Add(t3.Sub(t0) / 2)
	return mid.Sub(server)
}

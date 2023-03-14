package main

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
)

const (
	socksVersion = 5
	cmdConnect   = 1
	addrTypeIPv4 = 1
	addrTypeFQDN = 3
	addrTypeIPv6 = 4
)

func main() {
	listenAddr := "0.0.0.0:1085" // change this to the address you want to listen on
	httpProxy := "http://localhost:8888" // change this to your HTTP proxy

	// create the listener for incoming SOCKS connections
	listener, err := net.Listen("tcp", listenAddr)
	if err != nil {
		fmt.Println("error listening:", err)
		return
	}
	defer listener.Close()
	fmt.Println("listening on", listenAddr)

	for {
		// accept incoming SOCKS connections
		clientConn, err := listener.Accept()
		if err != nil {
			fmt.Println("error accepting connection:", err)
			continue
		}

		// handle each SOCKS connection in a new goroutine
		go handleClientConn(clientConn, httpProxy)
	}
}

func handleClientConn(clientConn net.Conn, httpProxy string) {


	// read the SOCKS version and method selection message
	buf := make([]byte, 3)
	_, err := io.ReadFull(clientConn, buf)
	if err != nil {
		fmt.Println("error reading version/selection:", err)
		return
	}

	// make sure we're using SOCKS version 5
	if buf[0] != socksVersion {
		fmt.Println("unsupported SOCKS version:", buf[0])
		return
	}

	clientConn.Write([]byte{buf[0], 0x00})

	buf = make([]byte, 1024)
	_, err = clientConn.Read( buf)
	if err != nil {
		fmt.Println("error reading version/selection:", err)
		return
	}

	if buf[1] != 0x01 {
		fmt.Fprintf(os.Stderr, "Unsupported SOCKS command: %d\n", buf[1])
		return
	}

	addrType := buf[3]
	addr := ""
	port := 0
	switch addrType {
	case 0x01: // IPv4 address
		addr = net.IPv4(buf[4], buf[5], buf[6], buf[7]).String()
		port = int(buf[8])<<8 + int(buf[9])
	case 0x03: // Domain name
		addrLen := int(buf[4])
		addr = string(buf[5 : 5+addrLen])
		port = int(buf[5+addrLen])<<8 + int(buf[5+addrLen+1])
	default:
		fmt.Fprintf(os.Stderr, "Unsupported address type: %d\n", addrType)
		return
	}

	// send a method selection response with the CONNECT command
	_, err = clientConn.Write([]byte{socksVersion, 0x00 , 0, addrTypeIPv4, 0, 0, 0, 0, 0, 0})
	if err != nil {
		fmt.Println("error sending method selection response:", err)
		return
	}

	// connect to the HTTP proxy
	httpProxyURL, err := url.Parse(httpProxy)
	if err != nil {
		fmt.Println("invalid HTTP proxy URL:", err)
		return
	}
	httpConn, err := net.Dial("tcp", httpProxyURL.Host)
	if err != nil {
		fmt.Println("error connecting to HTTP proxy:", err)
		return
	}


	// create the CONNECT request
	var reqBuf bytes.Buffer
	reqBuf.WriteString(fmt.Sprintf("CONNECT %s:%d HTTP/1.1\r\n", addr, port))
	reqBuf.WriteString(fmt.Sprintf("Host: %s:%d\r\n", addr, port))
	reqBuf.WriteString("Proxy-Connection: Keep-Alive\r\n")
	reqBuf.WriteString("\r\n")

	// send the CONNECT request to the HTTP proxy
	_, err = httpConn.Write(reqBuf.Bytes())
	if err != nil {
		fmt.Println("error sending CONNECT request to HTTP proxy:", err)
		return
	}

	// read the HTTP response to the CONNECT request
	respBuf := make([]byte, 1024)
	n, err := httpConn.Read(respBuf)
	if err != nil {
		fmt.Println("error reading response to CONNECT request from HTTP proxy:", err)
		return
	}
	respBuf = respBuf[:n]

	// check the HTTP response code
	respLines := bytes.Split(respBuf, []byte{'\r', '\n'})
	if len(respLines) < 1 {
		fmt.Println("empty response from HTTP proxy")
		return
	}
	firstLine := respLines[0]
	fields := bytes.Fields(firstLine)
	if len(fields) < 3 {
		fmt.Println("invalid response line from HTTP proxy:", string(firstLine))
		return
	}
	statusCode, err := strconv.Atoi(string(fields[1]))
	if err != nil {
		fmt.Println("invalid status code from HTTP proxy:", string(fields[1]))
		return
	}
	if statusCode != http.StatusOK {
		fmt.Println("HTTP proxy returned status code:", statusCode)
		return
	}



	// proxy data between the client and the HTTP proxy
	go func() {

		_, err = io.Copy(httpConn, clientConn)
		if err != nil {
			fmt.Println("error proxying data from client to HTTP proxy:", err)
		}
	}()
	go func() {
		// make sure the connection is closed when we're done
		defer clientConn.Close()
		defer httpConn.Close()

		_, err = io.Copy(clientConn, httpConn)
		if err != nil {
			fmt.Println("error proxying data from HTTP proxy to client:", err)
		}
	}()
}


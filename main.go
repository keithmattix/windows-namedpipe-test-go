//go:build windows
// +build windows

package main

import (
	"fmt"
	"log"
	"net"
	"os"
	"time"

	winio "github.com/Microsoft/go-winio"
	"google.golang.org/protobuf/proto"

	"github.com/keithmattix/windows-namedpipe-test-go/zdsapi"
)

var fakeWorkload = zdsapi.WorkloadInfo{
	Name:           "myapp",
	Namespace:      "default",
	ServiceAccount: "default",
}

func readProto[T any, PT interface {
	proto.Message
	*T
}](c winio.PipeConn, timeout time.Duration) (PT, int, error) {
	var buf [1024]byte
	err := c.SetReadDeadline(time.Now().Add(timeout))
	if err != nil {
		return nil, 0, err
	}
	n, err := c.Read(buf[:])
	if err != nil {
		return nil, 0, err
	}
	var resp T
	var respPtr PT = &resp
	err = proto.Unmarshal(buf[:n], respPtr)
	if err != nil {
		return nil, 0, err
	}
	return respPtr, n, nil
}

func handleConnection(conn net.Conn) error {
	fmt.Println("in the handler")
	log.Printf("Client connected [%s]", conn.RemoteAddr().String())
	pconn := conn.(winio.PipeConn)
	m, _, err := readProto[zdsapi.ZdsHello](pconn, 5*time.Second)
	if err != nil {
		return err
	}
	log.Printf("Received ZDS hello with version %s", m.GetVersion())
	// Send a fake response
	resp, err := sendMessageAndWaitForAck(pconn, &zdsapi.WorkloadRequest{
		Payload: &zdsapi.WorkloadRequest_Add{
			Add: &zdsapi.AddWorkload{
				Uid:                "1234-56-512512",
				WorkloadInfo:       &fakeWorkload,
				WindowsNamespaceId: "1234",
			},
		},
	})

	if err != nil {
		return err
	}

	log.Printf("Received response after Add workload request: %s", resp.String())
	return nil
}

func readMessage(conn winio.PipeConn, timeout time.Duration) (*zdsapi.WorkloadResponse, error) {
	m, _, err := readProto[zdsapi.WorkloadResponse](conn, timeout)
	return m, err
}

func sendMessageAndWaitForAck(conn winio.PipeConn, m *zdsapi.WorkloadRequest) (*zdsapi.WorkloadResponse, error) {
	data, err := proto.Marshal(m)
	if err != nil {
		return nil, err
	}

	return sendDataAndWaitForAck(conn, data)
}

func sendDataAndWaitForAck(conn winio.PipeConn, data []byte) (*zdsapi.WorkloadResponse, error) {
	err := conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	if err != nil {
		return nil, err
	}

	n, err := conn.Write(data)
	log.Printf("Sent %d bytes of data to client", n)
	if err != nil {
		return nil, err
	}

	return readMessage(conn, 5*time.Second)
}

func main() {
	pipePath := `\\.\pipe\istio-zds`

	if err := os.RemoveAll(pipePath); err != nil {
		log.Fatal(err)
	}

	pc := &winio.PipeConfig{
		SecurityDescriptor: "D:P(A;;GA;;;AU)",
		InputBufferSize:    1024,
		OutputBufferSize:   1024,
		MessageMode:        true,
	}

	l, err := winio.ListenPipe(pipePath, pc)

	if err != nil {
		log.Fatal("listen error:", err)
	}
	defer l.Close()

	for {
		conn, err := l.Accept()
		if err != nil {
			log.Fatal("accept error:", err)
		}
		go func() {
			log.Println("handling connection")
			if err := handleConnection(conn); err != nil {
				log.Fatalf("failed to handle conn: %s", err)
			}
		}()
	}
}

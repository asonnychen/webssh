package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"golang.org/x/crypto/ssh"
)

func main() {
	if len(os.Args) < 6 {
		fmt.Printf("%s <bindport> <sshhost:port> <sshuser> <sshpass> <sshconnecttime>\n", os.Args[0])
		return
	}
	httpConnectTime, err := strconv.ParseInt(os.Args[5], 10, 32)
	if err != nil {
		fmt.Printf("provide an int value as <sshconnecttime>:%s\n", os.Args[5])
		return
	}
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Connection") != "Upgrade" {
			w.Header().Add("Access-Control-Allow-Origin", "*")
			uri := strings.ReplaceAll(r.RequestURI, "..", "")
			http.ServeFile(w, r, "./"+strings.TrimLeft(uri, "/\\"))
			return
		}

		var upgrader = websocket.Upgrader{
			Subprotocols: []string{"binary"},
			CheckOrigin: func(r *http.Request) bool {
				return true
			},
		}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer func() {
			if conn != nil {
				conn.Close()
				conn = nil
			}
		}()

		sshHost := os.Args[2]
		sshUser := os.Args[3]
		sshPassword := os.Args[4]

		config := &ssh.ClientConfig{
			Timeout:         time.Duration(httpConnectTime) * time.Second,
			User:            sshUser,
			HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		}
		config.Auth = []ssh.AuthMethod{ssh.Password(sshPassword)}
		sshConn, err := ssh.Dial("tcp", sshHost, config)
		if err != nil {
			return
		}
		defer func() {
			if sshConn != nil {
				sshConn.Close()
			}
		}()
		session, err := sshConn.NewSession()
		if err != nil {
			return
		}
		defer func() {
			session.Close()
			session = nil
		}()
		modes := ssh.TerminalModes{
			ssh.ECHO:          1,
			ssh.TTY_OP_ISPEED: 14400,
			ssh.TTY_OP_OSPEED: 14400,
		}
		if err := session.RequestPty("xterm", 94, 31, modes); err != nil {
			return
		}

		sshReader, err := session.StdoutPipe()
		if err != nil {
			return
		}
		sshWriter, err := session.StdinPipe()
		if err != nil {
			return
		}
		defer func() {
			if sshWriter != nil {
				sshWriter.Close()
				sshWriter = nil
			}
		}()

		go func() {
			buf := make([]byte, 10240)
			for {
				if conn == nil || sshWriter == nil {
					break
				}

				n, err := sshReader.Read(buf)
				if err != nil {
					if sshWriter != nil {
						sshWriter.Close()
						sshWriter = nil
					}
					break
				}
				err = conn.WriteMessage(websocket.BinaryMessage, buf[:n])
				if err != nil {
					break
				}
			}
		}()

		go func(conn *websocket.Conn, sshWriter io.WriteCloser) {
			for {
				if conn == nil || sshWriter == nil {
					break
				}
				messageType, p, err := conn.ReadMessage()
				if err != nil && err != io.EOF {
					if conn != nil {
						conn.Close()
						conn = nil
					}
					break
				}
				if len(p) != 0 {
					if messageType == websocket.TextMessage {
						_, err = sshWriter.Write(p)
						if err != nil {
							if sshWriter != nil {
								sshWriter.Close()
								sshWriter = nil
							}
							break
						}
					} else if messageType == websocket.BinaryMessage {
						messages := strings.Split(string(p), ",")
						switch messages[0] {
						case "resize":
							if len(messages) == 3 {
								cols, err1 := strconv.ParseInt(messages[1], 10, 32)
								rows, err2 := strconv.ParseInt(messages[2], 10, 32)
								if err1 == nil && err2 == nil {
									session.WindowChange(int(rows), int(cols))
								}
							}
						default:
						}
					}
				}
			}
		}(conn, sshWriter)

		if err := session.Shell(); err != nil {
		}

		if err := session.Wait(); err != nil {
		}
	})

	serverBindAddr := "0.0.0.0:" + os.Args[1]
	if err := http.ListenAndServe(serverBindAddr, nil); err != nil {
	}
}

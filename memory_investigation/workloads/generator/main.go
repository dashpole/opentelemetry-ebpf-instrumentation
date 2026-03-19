package main

import (
	"context"
	"crypto/tls"
	"database/sql"
	"flag"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
	_ "github.com/go-sql-driver/mysql"
	_ "github.com/lib/pq"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"github.com/segmentio/kafka-go"
)

func main() {
	mode := flag.String("mode", "server", "server or client")
	protocol := flag.String("protocol", "http", "http, https, grpc, mysql, postgres, redis, kafka")
	target := flag.String("target", "localhost:8080", "target url/host for client")
	qps := flag.Int("qps", 10, "qps for client")
	flag.Parse()

	if *mode == "server" {
		switch *protocol {
		case "http", "https":
			http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				w.Write([]byte("OK"))
			})
			log.Printf("Starting HTTP(S) server on :8080")
			log.Fatal(http.ListenAndServe(":8080", nil))
		case "grpc":
			lis, err := net.Listen("tcp", ":9090")
			if err != nil { log.Fatalf("failed to listen: %v", err) }
			s := grpc.NewServer()
			healthServer := health.NewServer()
			healthServer.SetServingStatus("grpc.health.v1.Health", healthpb.HealthCheckResponse_SERVING)
			healthpb.RegisterHealthServer(s, healthServer)
			log.Printf("Starting gRPC server on :9090")
			log.Fatal(s.Serve(lis))
		default:
			// For other protocols, we rely on off-the-shelf images (e.g. redis:latest)
			log.Printf("Server mode not implemented natively for %s; relying on standard containers.", *protocol)
			select {}
		}
	} else if *mode == "client" {
		if *qps <= 0 {
			log.Printf("Idle client. Doing nothing.")
			select {} // Block forever
		}
		sleepDuration := time.Second / time.Duration(*qps)
		log.Printf("Starting %s client, target: %s, QPS: %d", *protocol, *target, *qps)

		var pingFunc func() error
		
		switch *protocol {
		case "http":
			client := http.Client{Timeout: 5 * time.Second}
			url := "http://" + *target + "/graphql"
			pingFunc = func() error {
				// Send a mock GraphQL payload to trigger HTTP_GRAPHQL extraction
				body := strings.NewReader(`{"query":"query { user(id: 1) { name } }"}`)
				req, err := http.NewRequest("POST", url, body)
				if err != nil { return err }
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("User-Agent", "obi-memory-investigation-client")
				// Simulated traceparent for context propagation extraction
				req.Header.Set("traceparent", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
				resp, err := client.Do(req)
				if err != nil { return err }
				defer resp.Body.Close()
				io.Copy(io.Discard, resp.Body)
				return nil
			}
		case "https":
			tr := &http.Transport{ TLSClientConfig: &tls.Config{InsecureSkipVerify: true} }
			client := http.Client{Timeout: 5 * time.Second, Transport: tr}
			url := "https://" + *target + "/graphql"
			pingFunc = func() error {
				// Send a mock GraphQL payload to trigger HTTP_GRAPHQL extraction
				body := strings.NewReader(`{"query":"query { user(id: 1) { name } }"}`)
				req, err := http.NewRequest("POST", url, body)
				if err != nil { return err }
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("User-Agent", "obi-memory-investigation-client")
				// Simulated traceparent for context propagation extraction
				req.Header.Set("traceparent", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
				resp, err := client.Do(req)
				if err != nil { return err }
				defer resp.Body.Close()
				io.Copy(io.Discard, resp.Body)
				return nil
			}
		case "redis":
			rdb := redis.NewClient(&redis.Options{ Addr: *target })
			pingFunc = func() error {
				// Send a SET and GET command to parse multi-bulk RESP arrays
				err := rdb.Set(context.Background(), "mykey", "myvalue", 0).Err()
				if err != nil { return err }
				return rdb.Get(context.Background(), "mykey").Err()
			}
		case "mysql":
			// dsn e.g. "test:test@tcp(localhost:3306)/testdb"
			db, err := sql.Open("mysql", *target)
			if err != nil { log.Fatal(err) }
			pingFunc = func() error {
				// Execute a query to trigger standard query parsing
				_, err := db.Query("SELECT 1 FROM DUAL WHERE 1 = 1")
				if err != nil { return err }
				
				// Execute a prepared statement to trigger PREPARE and EXECUTE LRU tracking
				stmt, err := db.Prepare("SELECT ? FROM DUAL")
				if err != nil { return err }
				defer stmt.Close()
				_, err = stmt.Exec(2)
				return err
			}
		case "postgres":
			// dsn e.g. "postgres://test:test@localhost:5432/testdb?sslmode=disable"
			db, err := sql.Open("postgres", *target)
			if err != nil { log.Fatal(err) }
			pingFunc = func() error {
				// Execute a basic query
				_, err := db.Query("SELECT 1 WHERE 1 = 1")
				if err != nil { return err }
				
				// Execute a prepared statement (triggers PARSE, BIND, EXECUTE)
				stmt, err := db.Prepare("SELECT $1::int")
				if err != nil { return err }
				defer stmt.Close()
				_, err = stmt.Exec(2)
				return err
			}
		case "grpc":
			conn, err := grpc.NewClient(*target, grpc.WithTransportCredentials(insecure.NewCredentials()))
			if err != nil { log.Fatal(err) }
			pingFunc = func() error {
				// Inject traceparent metadata to exercise http2grpc_transform extracting traceparent
				md := metadata.Pairs("traceparent", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
				ctx := metadata.NewOutgoingContext(context.Background(), md)
				
				// Use a dummy Invoke since we don't have generated Protobuf stubs here
				var reply string
				err := conn.Invoke(ctx, "/grpc.health.v1.Health/Check", &reply, &reply)
				
				// We expect an Unimplemented or similar error since there's no real server,
				// but the framing will still occur over HTTP/2, triggering OBI extractors.
				if err != nil && !strings.Contains(err.Error(), "Unimplemented") {
					// Ignore unimplemented errors, return others
				}
				return nil
			}
		case "kafka":
			topic := "test-topic"
			partition := 0
			
			// Setup connection
			conn, err := kafka.DialLeader(context.Background(), "tcp", *target, topic, partition)
			if err != nil {
				// The topic might not exist yet, ignoring dial errors for the stub init
				log.Printf("Kafka dial error: %v (will retry in loop)", err)
			} else {
				conn.Close()
			}
			
			w := &kafka.Writer{
				Addr:     kafka.TCP(*target),
				Topic:    topic,
				Balancer: &kafka.LeastBytes{},
			}
			
			r := kafka.NewReader(kafka.ReaderConfig{
				Brokers:   []string{*target},
				Topic:     topic,
				Partition: 0,
				MinBytes:  10,
				MaxBytes:  10e6,
			})

			pingFunc = func() error {
				// Produce message (Triggers APIKeyProduce)
				err := w.WriteMessages(context.Background(),
					kafka.Message{
						Key:   []byte("Key-A"),
						Value: []byte("Hello World!"),
					},
				)
				if err != nil {
					return err
				}
				
				// Fetch message (Triggers APIKeyFetch)
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				defer cancel()
				_, err = r.ReadMessage(ctx)
				if err != nil && !strings.Contains(err.Error(), "context deadline exceeded") {
					// Ignore timeout errors
					return err
				}
				return nil
			}
		default:
			log.Fatalf("Unknown protocol: %s", *protocol)
		}

		for {
			func() {
				err := pingFunc()
				if err != nil { log.Printf("Error: %v", err) }
			}()
			time.Sleep(sleepDuration)
		}
	} else {
		log.Fatal("Invalid mode. Use server or client")
	}
}

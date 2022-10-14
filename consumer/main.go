package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/Shopify/sarama"
	_ "github.com/jackc/pgx/v5/stdlib"
)

var topic string
var broker string
var dbHost string
var dbPassword string

const defaultBroker = "redpanda-0.redpanda.redpanda.svc.cluster.local.:9093"
const defaultTopic = "users"
const consumerGroup = "test-group"
const defaultDBHost = "postgres-demo-postgresql.default.svc.cluster.local"

var db *sql.DB

//postgres://username:password@host:5432/database_name
const dbURLFormat = "postgres://postgres:%s@%s:5432/postgres"

func init() {
	broker = os.Getenv("REDPANDA_BROKER")
	if broker == "" {
		broker = defaultBroker
		log.Println("using default value for redpanda broker", defaultBroker)
	}

	topic = os.Getenv("TOPIC")
	if topic == "" {
		topic = defaultTopic
		log.Println("using default value for redpanda topic", defaultTopic)
	}

	dbHost = os.Getenv("POSTGRESQL_HOST")
	if dbHost == "" {
		dbHost = defaultDBHost
		log.Println("using default value for database host", defaultDBHost)
	}

	dbPassword = os.Getenv("POSTGRESQL_PASSWORD")
	if dbPassword == "" {
		log.Fatal("missing value for environment variable POSTGRESQL_PASSWORD")
	}

	connURL := fmt.Sprintf(dbURLFormat, dbPassword, dbHost)
	log.Println("posgtres connection url", connURL)

	var err error
	db, err = sql.Open("pgx", connURL)
	if err != nil {
		log.Println("failed to open database", connURL, err)
	}

	err = db.Ping()
	if err != nil {
		log.Println("failed to connect to database", connURL, err)
	}
}

func main() {
	keepRunning := true
	log.Println("starting consumer")

	sarama.Logger = log.New(os.Stdout, "[sarama] ", log.LstdFlags)

	config := sarama.NewConfig()
	config.Consumer.Group.Rebalance.Strategy = sarama.BalanceStrategySticky
	config.Consumer.Offsets.Initial = sarama.OffsetOldest

	ctx, cancel := context.WithCancel(context.Background())
	client, err := sarama.NewConsumerGroup([]string{broker}, consumerGroup, config)
	if err != nil {
		log.Panicf("error creating consumer group client: %v", err)
	}

	consumer := SimpleConsumerHandler{
		ready: make(chan bool),
	}

	wg := &sync.WaitGroup{}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			err := client.Consume(ctx, []string{topic}, &consumer)
			if err != nil {
				log.Panicf("error joining consumer group: %v", err)
			}
			if ctx.Err() != nil {
				return
			}
			consumer.ready = make(chan bool)
		}
	}()

	<-consumer.ready
	log.Println("consumer ready")

	sigterm := make(chan os.Signal, 1)
	signal.Notify(sigterm, syscall.SIGINT, syscall.SIGTERM)

	for keepRunning {
		select {
		case <-ctx.Done():
			log.Println("terminating: context cancelled")
			keepRunning = false
		case <-sigterm:
			log.Println("terminating: via signal")
			keepRunning = false
		}
	}
	cancel()
	wg.Wait()

	err = client.Close()
	if err != nil {
		log.Panicf("error closing kafka client: %v", err)
	}
	err = db.Close()
	if err != nil {
		log.Panicf("error closing database client: %v", err)
	}
}

type SimpleConsumerHandler struct {
	ready chan bool
}

func (consumer *SimpleConsumerHandler) Setup(sarama.ConsumerGroupSession) error {
	close(consumer.ready)
	return nil
}

const insertSQL = "INSERT INTO users (email, username) VALUES ($1, $2)"

type User struct {
	Email    string `json:"email"`
	Username string `json:"name"`
}

func (consumer *SimpleConsumerHandler) ConsumeClaim(session sarama.ConsumerGroupSession, claim sarama.ConsumerGroupClaim) error {
	for {
		select {
		case message := <-claim.Messages():
			log.Printf("message received: value = %s, topic = %v, partition = %v, topic = %v", string(message.Value), message.Topic, message.Partition, message.Offset)

			var u User
			err := json.Unmarshal(message.Value, &u)
			if err != nil {
				log.Println("failed to unmarshal payload", err)
				return err
			}

			_, err = db.Exec(insertSQL, u.Email, u.Username)
			if err != nil {
				log.Println("failed to insert record in database", err)
				return err
			}

			log.Println("successfully added record to database", u)

			session.MarkMessage(message, "")

		case <-session.Context().Done():
			return nil
		}
	}
}

func (consumer *SimpleConsumerHandler) Cleanup(sarama.ConsumerGroupSession) error {
	return nil
}

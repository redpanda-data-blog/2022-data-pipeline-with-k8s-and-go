package main

import (
	"context"
	"io/ioutil"
	"log"
	"net/http"
	"os"

	"github.com/twmb/franz-go/pkg/kgo"
)

var topic string
var broker string

const defaultBroker = "redpanda-0.redpanda.redpanda.svc.cluster.local.:9093"
const defaultTopic = "users"

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
}

func main() {

	log.Println("redpanda broker", broker)

	client, err := kgo.NewClient(
		kgo.SeedBrokers(broker),
		kgo.RecordPartitioner(kgo.RoundRobinPartitioner()),
	)
	if err != nil {
		panic(err)
	}
	defer client.Close()

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		payload, err := ioutil.ReadAll(r.Body)

		if err != nil {
			log.Println("unable to read body")
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		log.Println("payload", string(payload))
		defer r.Body.Close()

		res := client.ProduceSync(context.Background(), &kgo.Record{Topic: topic, Value: payload})

		for _, r := range res {
			if r.Err != nil {
				log.Println("produce error:", r.Err)
				http.Error(w, r.Err.Error(), http.StatusInternalServerError)
				return
			}
			w.Header().Add("kafka-timestamp", r.Record.Timestamp.String())

			log.Printf("record produced: value = %s, topic = %v, partition = %v, topic = %v", string(r.Record.Value), r.Record.Topic, r.Record.Partition, r.Record.Offset)
		}
	})

	log.Println("http server init...")

	log.Fatal(http.ListenAndServe(":8080", nil))

}

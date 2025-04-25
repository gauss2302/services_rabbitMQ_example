package main

import (
	"fmt"
	"github.com/gorilla/mux"
	"log"
	"net/http"
	"os"
	"rabbit_example/common"
)

func main() {
	rabbitURL := os.Getenv("RABBIT_URL")
	if rabbitURL == "" {
		rabbitURL = "amqp://guest:guest@rabbitmq:5672/"
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8082"
	}

	rabbitConfig := common.RabbitMQConfig{
		URL:      rabbitURL,
		Exchange: common.OrderExchange,
	}

	log.Printf("Connecting to RabbitMQ at %s...", rabbitURL)
	rabbitClient, err := common.NewRabbitMQClient(rabbitConfig)
	if err != nil {
		log.Fatalf("Failed to initialize RabbitMQ client: %v", err)
	}
	defer rabbitClient.Close()

	purchaseService := NewPurchaseService(rabbitClient)

	router := mux.NewRouter()
	router.HandleFunc("/orders", purchaseService.PlaceOrder).Methods("POST")
	router.HandleFunc("/health", purchaseService.HealthCheck).Methods("GET")

	log.Printf("Purchase service starting on port %s...", port)
	log.Fatal(http.ListenAndServe(fmt.Sprintf(":%s", port), router))
}

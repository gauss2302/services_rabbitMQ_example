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
	rabbitURL := os.Getenv("RABBITMQ_URL")
	if rabbitURL == "" {
		rabbitURL = "amqp://guest:guest@rabbitmq:5672/"
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8081"
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

	warehouseService := NewWarehouseService(rabbitClient)
	if err := warehouseService.StartConsumers(); err != nil {
		log.Fatalf("Failed to start consumers: %v", err)
	}

	router := mux.NewRouter()
	router.HandleFunc("/inventory", warehouseService.GetInventory).Methods("GET")
	router.HandleFunc("/health", warehouseService.HealthCheck).Methods("GET")

	log.Printf("Warehouse service starting on port %s...", port)
	go func() {
		log.Fatal(http.ListenAndServe(fmt.Sprintf(":%s", port), router))
	}()

	forever := make(chan struct{})
	log.Printf("Warehouse service is running. Press CTRL+C to exit.")
	<-forever
}

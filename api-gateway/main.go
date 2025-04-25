package main

import (
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

	purchaseURL := os.Getenv("PURCHASE_SERVICE_URL")
	if purchaseURL == "" {
		purchaseURL = "http://purchase-service:8080"
	}

	warehouseURL := os.Getenv("WAREHOUSE_SERVICE_URL")
	if warehouseURL == "" {
		warehouseURL = "http://warehouse-service:8081"
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8000"
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

	gatewayHandler := NewGatewayHandler(purchaseURL, warehouseURL, rabbitClient)

	router := mux.NewRouter()
	router.HandleFunc("/api/orders", gatewayHandler.CreateOrder).Methods("POST")
	router.HandleFunc("/api/orders/status", gatewayHandler.GetOrderStatus).Methods("GET")
	router.HandleFunc("/api/inventory", gatewayHandler.GetInventory).Methods("GET")
	router.HandleFunc("/health", gatewayHandler.Health).Methods("GET")

	log.Printf("API Gateway starting on port %s...", port)
	log.Fatal(http.ListenAndServe(":"+port, router))
}

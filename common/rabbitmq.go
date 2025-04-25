package common

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

const (
	// Queue names
	OrderPlacedQueue = "order_placed_queue"
	OrderStatusQueue = "order_status_queue"

	// Exchange names
	OrderExchange = "order_exchange"

	// Routing keys
	OrderPlacedRoutingKey = "order.placed"
	OrderStatusRoutingKey = "order.status"
)

// RabbitMQConfig holds the configuration for RabbitMQ
type RabbitMQConfig struct {
	URL      string
	Exchange string
}

// RabbitMQClient is a wrapper around RabbitMQ connection
type RabbitMQClient struct {
	conn    *amqp.Connection
	channel *amqp.Channel
	Config  RabbitMQConfig // Note: Capital 'C' to export the field
}

func (c *RabbitMQClient) GetChannel() (*amqp.Channel, error) {
	if c.conn == nil {
		return nil, fmt.Errorf("connection is nil")
	}
	return c.conn.Channel()
}

// NewRabbitMQClient creates a new RabbitMQ client with connection retry
func NewRabbitMQClient(config RabbitMQConfig) (*RabbitMQClient, error) {
	var conn *amqp.Connection
	var err error

	// Retry connection up to 5 times with exponential backoff
	for i := 0; i < 5; i++ {
		conn, err = amqp.Dial(config.URL)
		if err == nil {
			break
		}
		retryTime := time.Duration(i*i)*time.Second + time.Second
		log.Printf("Failed to connect to RabbitMQ, retrying in %v: %v", retryTime, err)
		time.Sleep(retryTime)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to connect to RabbitMQ after retries: %w", err)
	}

	channel, err := conn.Channel()
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to open channel: %w", err)
	}

	// Ensure the exchange exists
	if config.Exchange == "" {
		return nil, fmt.Errorf("exchange name cannot be empty")
	}

	// Declare the exchange
	err = channel.ExchangeDeclare(
		config.Exchange, // name
		"topic",         // type (topic allows for routing with patterns)
		true,            // durable
		false,           // auto-deleted
		false,           // internal
		false,           // no-wait
		nil,             // arguments
	)
	if err != nil {
		channel.Close()
		conn.Close()
		return nil, fmt.Errorf("failed to declare exchange %s: %w", config.Exchange, err)
	}
	log.Printf("Successfully declared exchange: %s", config.Exchange)

	// Pre-declare queues that we know we'll need
	queues := []string{OrderPlacedQueue, OrderStatusQueue}
	routingKeys := []string{OrderPlacedRoutingKey, OrderStatusRoutingKey}

	for i, queueName := range queues {
		// Declare the queue
		q, err := channel.QueueDeclare(
			queueName, // name
			true,      // durable
			false,     // delete when unused
			false,     // exclusive
			false,     // no-wait
			nil,       // arguments
		)
		if err != nil {
			channel.Close()
			conn.Close()
			return nil, fmt.Errorf("failed to declare queue %s: %w", queueName, err)
		}
		log.Printf("Successfully declared queue: %s", queueName)

		// Bind the queue to the exchange
		err = channel.QueueBind(
			q.Name,          // queue name
			routingKeys[i],  // routing key
			config.Exchange, // exchange
			false,           // no-wait
			nil,             // arguments
		)
		if err != nil {
			channel.Close()
			conn.Close()
			return nil, fmt.Errorf("failed to bind queue %s to exchange %s: %w",
				queueName, config.Exchange, err)
		}
		log.Printf("Successfully bound queue %s to exchange %s with routing key %s",
			queueName, config.Exchange, routingKeys[i])
	}

	client := &RabbitMQClient{
		conn:    conn,
		channel: channel,
		Config:  config, // Using the exported field
	}

	return client, nil
}

// Close closes the RabbitMQ connection and channel
func (c *RabbitMQClient) Close() error {
	if c.channel != nil {
		if err := c.channel.Close(); err != nil {
			return err
		}
	}
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

// PublishMessage publishes a message to the specified routing key
func (c *RabbitMQClient) PublishMessage(ctx context.Context, routingKey string, message interface{}) error {
	messageBytes, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}

	err = c.channel.PublishWithContext(
		ctx,
		c.Config.Exchange, // Using the exported field
		routingKey,        // routing key
		false,             // mandatory
		false,             // immediate
		amqp.Publishing{
			ContentType:  "application/json",
			DeliveryMode: amqp.Persistent, // make message persistent
			Timestamp:    time.Now(),
			Body:         messageBytes,
		},
	)
	if err != nil {
		return fmt.Errorf("failed to publish message to exchange %s with routing key %s: %w",
			c.Config.Exchange, routingKey, err)
	}

	log.Printf("Successfully published message to exchange %s with routing key %s",
		c.Config.Exchange, routingKey)
	return nil
}

// ConsumeMessages sets up a consumer for the specified queue and routing key
func (c *RabbitMQClient) ConsumeMessages(queueName, routingKey string, handler func([]byte) error, exchange string) error {
	// Validate exchange name
	if exchange == "" {
		return fmt.Errorf("exchange name cannot be empty")
	}

	// The queue should already be declared in the constructor, but let's verify it exists
	q, err := c.channel.QueueInspect(queueName)
	if err != nil {
		return fmt.Errorf("queue %s does not exist: %w", queueName, err)
	}

	log.Printf("Found queue %s with %d messages", queueName, q.Messages)

	// Bind the queue to the specified exchange with the routing key
	err = c.channel.QueueBind(
		q.Name,     // queue name
		routingKey, // routing key
		exchange,   // exchange - using the provided parameter
		false,      // no-wait
		nil,        // arguments
	)
	if err != nil {
		return fmt.Errorf("failed to bind queue %s to exchange %s: %w",
			queueName, exchange, err)
	}
	log.Printf("Successfully bound queue %s to exchange %s with routing key %s",
		queueName, exchange, routingKey)

	// Set QoS to limit the number of unacknowledged messages
	err = c.channel.Qos(
		1,     // prefetch count
		0,     // prefetch size
		false, // global
	)
	if err != nil {
		return fmt.Errorf("failed to set QoS: %w", err)
	}

	// Start consuming
	msgs, err := c.channel.Consume(
		queueName, // queue
		"",        // consumer
		false,     // auto-ack
		false,     // exclusive
		false,     // no-local
		false,     // no-wait
		nil,       // args
	)
	if err != nil {
		return fmt.Errorf("failed to start consuming from queue %s: %w", queueName, err)
	}

	go func() {
		for msg := range msgs {
			log.Printf("Received message with routing key: %s", msg.RoutingKey)
			err := handler(msg.Body)
			if err != nil {
				log.Printf("Error handling message: %v", err)
				// Negative acknowledgement, message will be requeued
				msg.Nack(false, true)
			} else {
				// Acknowledge message
				msg.Ack(false)
			}
		}
	}()

	log.Printf("Successfully started consumer for queue %s with routing key %s",
		queueName, routingKey)
	return nil
}

// SetupOrderQueue declares and binds the order queue
func (c *RabbitMQClient) SetupOrderQueue() error {
	// Create a new channel for this operation
	ch, err := c.conn.Channel()
	if err != nil {
		return fmt.Errorf("failed to open channel: %w", err)
	}
	defer ch.Close()

	// Ensure the exchange exists
	exchangeName := c.Config.Exchange
	log.Printf("Ensuring exchange exists: %s", exchangeName)

	err = ch.ExchangeDeclare(
		exchangeName, // name
		"topic",      // type
		true,         // durable
		false,        // auto-deleted
		false,        // internal
		false,        // no-wait
		nil,          // arguments
	)
	if err != nil {
		return fmt.Errorf("failed to declare exchange: %w", err)
	}

	// Declare the order placed queue
	queueName := OrderPlacedQueue
	log.Printf("Declaring queue: %s", queueName)

	q, err := ch.QueueDeclare(
		queueName, // name
		true,      // durable
		false,     // delete when unused
		false,     // exclusive
		false,     // no-wait
		nil,       // arguments
	)
	if err != nil {
		return fmt.Errorf("failed to declare queue: %w", err)
	}

	// Bind the queue to the exchange
	routingKey := OrderPlacedRoutingKey
	log.Printf("Binding queue %s to exchange %s with routing key %s",
		queueName, exchangeName, routingKey)

	err = ch.QueueBind(
		q.Name,       // queue name
		routingKey,   // routing key
		exchangeName, // exchange
		false,        // no-wait
		nil,          // arguments
	)
	if err != nil {
		return fmt.Errorf("failed to bind queue: %w", err)
	}

	log.Printf("Queue %s successfully set up", queueName)
	return nil
}

// ConsumeOrderQueue consumes messages from the order queue with a custom handler
func (c *RabbitMQClient) ConsumeOrderQueue(handler func([]byte) error) error {
	// Create a new channel for consuming
	ch, err := c.conn.Channel()
	if err != nil {
		return fmt.Errorf("failed to open channel: %w", err)
	}

	// Set QoS
	err = ch.Qos(
		1,     // prefetch count
		0,     // prefetch size
		false, // global
	)
	if err != nil {
		ch.Close()
		return fmt.Errorf("failed to set QoS: %w", err)
	}

	// Start consuming from the queue
	queueName := OrderPlacedQueue
	log.Printf("Starting to consume from queue: %s", queueName)

	msgs, err := ch.Consume(
		queueName, // queue
		"",        // consumer
		false,     // auto-ack
		false,     // exclusive
		false,     // no-local
		false,     // no-wait
		nil,       // args
	)
	if err != nil {
		ch.Close()
		return fmt.Errorf("failed to start consuming: %w", err)
	}

	// Start goroutine to handle messages
	go func() {
		defer ch.Close() // Ensure channel gets closed when goroutine exits

		for msg := range msgs {
			log.Printf("Received message with routing key: %s", msg.RoutingKey)
			err := handler(msg.Body)
			if err != nil {
				log.Printf("Error handling message: %v", err)
				// Negative acknowledgement, message will be requeued
				msg.Nack(false, true)
			} else {
				// Acknowledge message
				msg.Ack(false)
			}
		}
	}()

	return nil
}

// Add this to your common/rabbitmq.go file

// EnsureExchange ensures that an exchange exists and is not empty
func EnsureExchange(exchangeName string) string {
	if exchangeName == "" {
		// If empty, use a default value instead of the empty string (default exchange)
		return "order_exchange"
	}
	return exchangeName
}

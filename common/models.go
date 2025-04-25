package common

type OrderPlaced struct {
	OrderID    string  `json:"order_id"`
	CustomerID string  `json:"customer_id"`
	Items      []Item  `json:"items"`
	TotalPrice float64 `json:"total_price"`
	CreatedAt  string  `json:"created_at"`
}

type Item struct {
	ProductID string  `json:"product_id"`
	Quantity  int     `json:"quantity"`
	Price     float64 `json:"price"`
}

type OrderStatus struct {
	OrderID    string `json:"order_id"`
	Status     string `json:"status"`
	Message    string `json:"message"`
	CustomerID string `json:"customer_id,omitempty"`
	CreatedAt  string `json:"created_at,omitempty"`
}

type InventoryItem struct {
	ProductID string  `json:"product_id"`
	Name      string  `json:"name"`
	Stock     int     `json:"stock"`
	Price     float64 `json:"price,omitempty"`
}

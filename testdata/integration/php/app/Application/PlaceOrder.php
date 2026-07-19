<?php
namespace App\Application;

use App\Domain\Order;

class PlaceOrder {
    public function create(string $id): Order {
        return new Order($id);
    }
}

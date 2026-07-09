<?php

namespace App\Infrastructure;

use App\Application\PlaceOrder;
use App\Domain\Order;

// Infrastructure entrypoint. Facade: OrderRepository.
final class OrderRepository
{
    public function save(Order $o): void
    {
    }

    public function place(): PlaceOrder
    {
        return new PlaceOrder();
    }
}

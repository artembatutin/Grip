<?php

namespace App\Application;

use App\Domain\Order;

// Application entrypoint. Facade: PlaceOrder.
final class PlaceOrder
{
    public function run(string $id): Order
    {
        return new Order($id);
    }
}

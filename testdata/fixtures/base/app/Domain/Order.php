<?php

namespace App\Domain;

// Domain entrypoint. Facade: Order.
final class Order
{
    public function __construct(public readonly string $id)
    {
    }
}

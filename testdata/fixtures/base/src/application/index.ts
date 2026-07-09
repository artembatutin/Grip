import { Order } from "../domain";

// Application entrypoint. Facade: PlaceOrder.
export class PlaceOrder {
  run(id: string): Order {
    return new Order(id);
  }
}

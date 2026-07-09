import { Order } from "../domain";
import { PlaceOrder } from "../application";

// Infrastructure entrypoint. Facade: OrderRepo.
export class OrderRepo {
  save(o: Order): void {}
  place(): PlaceOrder {
    return new PlaceOrder();
  }
}

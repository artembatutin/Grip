import { Order } from "../domain";

export class PlaceOrder {
  create(id: string): Order {
    return new Order(id);
  }
}

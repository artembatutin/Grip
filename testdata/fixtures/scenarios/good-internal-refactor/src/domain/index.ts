// Internal refactor: the implementation changed but the module boundary (the
// exported Order class, its facade) did not. The gate must stay green.
export class Order {
  private readonly value: string;
  constructor(id: string) {
    this.value = id.trim();
  }
  get id(): string {
    return this.value;
  }
}

// Domain entrypoint. The public surface is declared in grip.yaml (facade: Order).
export class Order {
  constructor(public readonly id: string) {}
}

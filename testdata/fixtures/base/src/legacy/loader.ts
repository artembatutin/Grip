// legacy is deliberately UNGOVERNED (no grip.yaml) to exercise FR-14, and it
// contains a dynamic import() the analyzer cannot statically resolve — a
// reduced-confidence scope. Because legacy is ungoverned, that reduced scope
// does not gate anything (it exercises that reduced confidence OUTSIDE a
// governed module is correctly ignored).
export async function load(name: string): Promise<unknown> {
  return import(`./plugins/${name}`);
}

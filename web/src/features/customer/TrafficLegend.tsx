/** One sentence that pins down the two directions, shown wherever traffic numbers appear. */
export function TrafficLegend() {
  return (
    <p className="text-xs leading-5 text-muted">
      <span className="font-medium text-fg">Received from tasks</span> is what tasks sent to your service through the gateway.{" "}
      <span className="font-medium text-fg">Sent to tasks</span> is what your service sent back. Counted by your own gateway, in bytes.
    </p>
  );
}

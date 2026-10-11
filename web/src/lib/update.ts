// The guided update (task 8.16). Deliberately not a button that pushes code onto a machine: an agent or gateway runs on the
// owner's own hardware and only ever connects out, so replacing it is the owner's decision. This builds the commands to do it.

export type UpdateKind = "agent" | "gateway";

const IMAGE: Record<UpdateKind, string> = {
  agent: "docker.io/fattymango/lazycake-agent:latest",
  gateway: "docker.io/fattymango/lazycake-gateway:latest",
};

const CONTAINER: Record<UpdateKind, string> = { agent: "lazycake-agent", gateway: "lazycake-gateway" };

/**
 * Pull the new image, then recreate the container with exactly the command it was created with (podman records it), so
 * the token, offer and every other setting carry over and nothing has to be typed again. The recorded command is read
 * before the old container is removed (a container created by hand may not say --replace, and the name would be taken).
 */
export function updateCommand(kind: UpdateKind): string {
  const name = CONTAINER[kind];
  const recreate = `import json,subprocess,sys; c=json.load(sys.stdin); subprocess.run(["podman","rm","-f","${name}"],check=True); subprocess.run(c,check=True)`;
  return [`podman pull ${IMAGE[kind]}`, `podman inspect ${name} --format '{{json .Config.CreateCommand}}' | python3 -c '${recreate}'`].join(
    "\n"
  );
}

/** What to show next to a version: null when there is nothing to say. */
export function updateLabel(state: "current" | "outdated" | "unknown"): string | null {
  if (state === "outdated") return "Update available";
  if (state === "unknown") return "Version unknown";
  return null;
}

export function describeVersion(version: string): string {
  return version === "" ? "not reported" : version;
}

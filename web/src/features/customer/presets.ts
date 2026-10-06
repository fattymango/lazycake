// Starting points on the New task page. To add one, add an entry: nothing
// else needs to change. Images must be pinned by digest.
export interface TaskPreset {
  id: string;
  name: string;
  description: string;
  image: string;
  /** Command line; empty means the image's own entrypoint. */
  command: string;
  env?: Record<string, string>;
  cores: number;
  memoryMB: number;
  timeoutS: number;
}

export const presets: TaskPreset[] = [
  {
    id: "benchmark",
    name: "Benchmark",
    description: "Burns CPU and memory and prints usage next to your limits, to show they're enforced.",
    image: "docker.io/fattymango/lcbench@sha256:65e8c5765cc43af8cf9e6a71165d31954dbfee495aa1ada997fb35b8c672e1db",
    command: "",
    env: { LCBENCH_DURATION: "60s" },
    cores: 0.5,
    memoryMB: 512,
    timeoutS: 300,
  },
  {
    id: "hello",
    name: "Hello world",
    description: "Prints a line and exits. The quickest way to see a task run end to end.",
    image: "docker.io/library/alpine@sha256:c64c687cbea9300178b30c95835354e34c4e4febc4badfe27102879de0483b5e",
    command: 'echo "hello from a stranger\'s machine"',
    cores: 0.25,
    memoryMB: 128,
    timeoutS: 60,
  },
  {
    id: "sleep",
    name: "Sleep for a minute",
    description: "Holds a slot for 60 seconds. Useful for watching scheduling and capacity.",
    image: "docker.io/library/alpine@sha256:c64c687cbea9300178b30c95835354e34c4e4febc4badfe27102879de0483b5e",
    command: "sleep 60",
    cores: 0.25,
    memoryMB: 128,
    timeoutS: 120,
  },
];

export const CORE_OPTIONS = [0.25, 0.5, 1, 2, 4];
export const MEMORY_OPTIONS = [128, 256, 512, 1024, 2048, 4096];
export const TIMEOUT_OPTIONS = [
  { value: 60, label: "1 min" },
  { value: 300, label: "5 min" },
  { value: 900, label: "15 min" },
  { value: 3600, label: "1 hour" },
  { value: 21600, label: "6 hours" },
];

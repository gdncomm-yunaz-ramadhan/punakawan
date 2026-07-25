<script lang="ts">
  export type IconName =
    | "activity"
    | "alert"
    | "approval"
    | "book"
    | "chart"
    | "check"
    | "clock"
    | "code"
    | "comment"
    | "dashboard"
    | "database"
    | "diff"
    | "file"
    | "folder"
    | "git-branch"
    | "heart"
    | "info"
    | "list"
    | "search"
    | "server"
    | "settings"
    | "sun"
    | "moon"
    | "monitor"
    | "users"
    | "workspace"
    | "x";

  interface Props {
    name: IconName;
    size?: number;
    strokeWidth?: number;
    label?: string;
  }

  let { name, size = 18, strokeWidth = 1.8, label }: Props = $props();

  const paths: Record<IconName, string[]> = {
    activity: ["M3 12h4l2.5-7 5 14 2.5-7H21"],
    alert: ["M12 9v4", "M12 17h.01", "M10.3 3.7 2.5 17.2A2 2 0 0 0 4.2 20h15.6a2 2 0 0 0 1.7-2.8L13.7 3.7a2 2 0 0 0-3.4 0Z"],
    approval: ["M12 3 5 6v5c0 4.6 2.8 8 7 10 4.2-2 7-5.4 7-10V6l-7-3Z", "m9.5 12 1.7 1.7 3.5-3.7"],
    book: ["M4 5.5A2.5 2.5 0 0 1 6.5 3H11v17H6.5A2.5 2.5 0 0 0 4 22V5.5Z", "M20 5.5A2.5 2.5 0 0 0 17.5 3H13v17h4.5A2.5 2.5 0 0 1 20 22V5.5Z"],
    chart: ["M4 19V9", "M10 19V5", "M16 19v-7", "M22 19H2"],
    check: ["M20 6 9 17l-5-5"],
    clock: ["M12 22a10 10 0 1 0 0-20 10 10 0 0 0 0 20Z", "M12 6v6l4 2"],
    code: ["m8 9-3 3 3 3", "m16 9 3 3-3 3", "m14 5-4 14"],
    comment: ["M21 15a4 4 0 0 1-4 4H8l-5 3V7a4 4 0 0 1 4-4h10a4 4 0 0 1 4 4v8Z"],
    dashboard: ["M4 13h6V4H4v9Z", "M14 20h6v-9h-6v9Z", "M14 7h6V4h-6v3Z", "M4 20h6v-3H4v3Z"],
    database: ["M20 6c0 2-3.6 3-8 3S4 8 4 6s3.6-3 8-3 8 1 8 3Z", "M4 6v6c0 2 3.6 3 8 3s8-1 8-3V6", "M4 12v6c0 2 3.6 3 8 3s8-1 8-3v-6"],
    diff: ["M6 3v14", "m3 14-3 3-3-3", "M18 21V7", "m-3 0 3-3 3 3"],
    file: ["M6 2h8l4 4v16H6V2Z", "M14 2v5h5", "M9 13h6", "M9 17h6"],
    folder: ["M3 5h7l2 2h9v12H3V5Z"],
    "git-branch": ["M6 3v12", "M18 9a6 6 0 0 1-6 6H6", "M6 3a2 2 0 1 0 0 .01Z", "M6 19a2 2 0 1 0 0 .01Z", "M18 7a2 2 0 1 0 0 .01Z"],
    heart: ["M20.8 4.6a5.5 5.5 0 0 0-7.8 0L12 5.7l-1.1-1.1a5.5 5.5 0 0 0-7.8 7.8L12 21l8.8-8.6a5.5 5.5 0 0 0 0-7.8Z", "M7 12h3l1-3 2 6 1-3h3"],
    info: ["M12 22a10 10 0 1 0 0-20 10 10 0 0 0 0 20Z", "M12 10v6", "M12 7h.01"],
    list: ["M9 6h11", "M9 12h11", "M9 18h11", "M4 6h.01", "M4 12h.01", "M4 18h.01"],
    search: ["M11 19a8 8 0 1 1 0-16 8 8 0 0 1 0 16Z", "m21 21-4.35-4.35"],
    server: ["M4 3h16v7H4V3Z", "M4 14h16v7H4v-7Z", "M8 6.5h.01", "M8 17.5h.01"],
    settings: ["M12 15.5a3.5 3.5 0 1 0 0-7 3.5 3.5 0 0 0 0 7Z", "M19.4 15a1.7 1.7 0 0 0 .34 1.88l.06.06-2.83 2.83-.06-.06a1.7 1.7 0 0 0-1.88-.34 1.7 1.7 0 0 0-1.03 1.55V21H10v-.08a1.7 1.7 0 0 0-1.03-1.55 1.7 1.7 0 0 0-1.88.34l-.06.06-2.83-2.83.06-.06A1.7 1.7 0 0 0 4.6 15a1.7 1.7 0 0 0-1.55-1.03H3v-4h.08A1.7 1.7 0 0 0 4.63 8.94a1.7 1.7 0 0 0-.34-1.88L4.23 7 7.06 4.17l.06.06A1.7 1.7 0 0 0 9 4.57a1.7 1.7 0 0 0 1-1.55V3h4v.08a1.7 1.7 0 0 0 1.03 1.55 1.7 1.7 0 0 0 1.88-.34l.06-.06L19.8 7l-.06.06a1.7 1.7 0 0 0-.34 1.88A1.7 1.7 0 0 0 20.95 10H21v4h-.08A1.7 1.7 0 0 0 19.4 15Z"],
    sun: ["M12 4V2", "M12 22v-2", "m4.93 4.93 1.42-1.42", "m5.66 18.35 1.41-1.41", "M20 12h2", "M2 12h2", "m16.93 16.93 1.42 1.42", "m3.51 3.51 1.42 1.42", "M12 17a5 5 0 1 0 0-10 5 5 0 0 0 0 10Z"],
    moon: ["M21 12.8A9 9 0 1 1 11.2 3 7 7 0 0 0 21 12.8Z"],
    monitor: ["M3 4h18v13H3V4Z", "M8 21h8", "M12 17v4"],
    users: ["M16 21v-2a4 4 0 0 0-4-4H6a4 4 0 0 0-4 4v2", "M9 11a4 4 0 1 0 0-8 4 4 0 0 0 0 8Z", "M22 21v-2a4 4 0 0 0-3-3.87", "M16 3.13a4 4 0 0 1 0 7.75"],
    workspace: ["M3 4h18v16H3V4Z", "M8 4v16", "M8 9h13"],
    x: ["M18 6 6 18", "m6 6 12 12"],
  };
</script>

<svg
  xmlns="http://www.w3.org/2000/svg"
  width={size}
  height={size}
  viewBox="0 0 24 24"
  fill="none"
  stroke="currentColor"
  stroke-width={strokeWidth}
  stroke-linecap="round"
  stroke-linejoin="round"
  aria-hidden={label ? undefined : "true"}
  aria-label={label}
  role={label ? "img" : undefined}
>
  {#each paths[name] as path}
    <path d={path}></path>
  {/each}
</svg>

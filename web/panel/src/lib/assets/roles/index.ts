// Wayang role portraits (Setara visual language) used wherever the four
// Punakawan roles are shown. Vite fingerprints and bundles each import, so
// callers get a stable hashed URL. Keyed by the canonical role id.
import semar from "./semar.png";
import gareng from "./gareng.png";
import petruk from "./petruk.png";
import bagong from "./bagong.png";

export type RoleId = "semar" | "gareng" | "petruk" | "bagong";

export const roleAvatars: Record<RoleId, string> = {
  semar,
  gareng,
  petruk,
  bagong,
};

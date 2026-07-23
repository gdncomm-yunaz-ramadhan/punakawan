// Shared DataTable/MobileDataList types (UI-011, §13.7).

export interface Column<T> {
  key: string;
  label: string;
  sortable?: boolean;
  render?: (row: T) => string;
  align?: "left" | "right" | "center";
  // Marks the column shown as each mobile card's title/primary field.
  primary?: boolean;
}

export type SortDirection = "asc" | "desc";

export interface SortState {
  key: string;
  direction: SortDirection;
}

export interface RowAction<T> {
  label: string;
  onSelect: (row: T) => void;
}

export function getCellValue<T>(row: T, column: Column<T>): string {
  if (column.render) return column.render(row);
  const value = (row as Record<string, unknown>)[column.key];
  if (value === null || value === undefined) return "";
  return String(value);
}

// What the title-bar filter means for one row.

/**
 * matchesFilter reports whether a row's text answers the filter. Every word in
 * the query has to appear somewhere in the row, in any order.
 */
export function matchesFilter(filter: string, ...text: (string | undefined)[]): boolean {
  const query = filter.trim().toLowerCase();
  if (query === "") return true;
  const hay = text
    .filter((t) => t !== undefined)
    .join(" ")
    .toLowerCase();
  return query.split(/\s+/).every((term) => hay.includes(term));
}

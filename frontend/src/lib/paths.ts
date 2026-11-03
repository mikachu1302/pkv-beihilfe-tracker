// Routes of the app that several pages link to.

/** The form of an invoice. */
export function invoicePath(id: string): string {
    return `/rechnung/${encodeURIComponent(id)}`;
}

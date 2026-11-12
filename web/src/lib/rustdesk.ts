// Links that open the RustDesk app installed on the viewer's computer. The client
// registers the rustdesk:// scheme and maps rustdesk://<action>/<id> to
// `rustdesk --<action> <id>` (urlLinkToCmdArgs in flutter/lib/common.dart).
// No password goes in the link: it would end up in browser history.

export type RustDeskAction = "connect" | "file-transfer" | "view-camera" | "terminal";

export const rustdeskLink = (action: RustDeskAction, id: string) => `rustdesk://${action}/${encodeURIComponent(id)}`;

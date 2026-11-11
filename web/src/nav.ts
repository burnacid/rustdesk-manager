import {
  LayoutDashboard, Monitor, FolderTree, Users, BookUser, ShieldCheck,
  ScrollText, Video, Settings, type LucideIcon,
} from "lucide-react";

export type NavItem = { path: string; label: string; icon: LucideIcon };

// Sidebar entries.
export const navItems: NavItem[] = [
  { path: "/", label: "Dashboard", icon: LayoutDashboard },
  { path: "/devices", label: "Devices", icon: Monitor },
  { path: "/device-groups", label: "Device groups", icon: FolderTree },
  { path: "/users", label: "Users", icon: Users },
  { path: "/address-books", label: "Address books", icon: BookUser },
  { path: "/strategies", label: "Strategies", icon: ShieldCheck },
  { path: "/audit", label: "Audit log", icon: ScrollText },
  { path: "/recordings", label: "Recordings", icon: Video },
  { path: "/settings", label: "Settings", icon: Settings },
];

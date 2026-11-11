import { useEffect, useState } from "react";
import { NavLink, Outlet, useLocation } from "react-router";
import { LogOut, Menu, UserCircle, X } from "lucide-react";
import { navItems } from "../nav";
import { AuthGate, useLogout, useUser } from "../auth";
import { Account } from "../pages/Account";
import { Logo } from "./Logo";
import { ThemeSwitcher } from "./ThemeSwitcher";

export function Layout() {
  return (
    <AuthGate>
      <Shell />
    </AuthGate>
  );
}

const linkClass = ({ isActive }: { isActive: boolean }) =>
  `flex min-h-11 w-full items-center gap-3 rounded-md px-3 text-base md:min-h-9 md:text-sm ${
    isActive
      ? "bg-teal-50 font-medium text-teal-700 dark:bg-teal-950 dark:text-teal-300"
      : "text-slate-600 hover:bg-slate-100 dark:text-slate-400 dark:hover:bg-slate-800"
  }`;

function NavLinks() {
  const user = useUser();
  const logout = useLogout();
  // Users who aren't admins only manage their own account here.
  const items = user.is_admin ? navItems : [];
  return (
    <nav className="flex flex-1 flex-col gap-1 overflow-y-auto px-2 pb-2" aria-label="Main">
      {items.map(({ path, label, icon: Icon }) => (
        <NavLink key={path} to={path} end={path === "/"} className={linkClass}>
          <Icon size={18} aria-hidden />
          {label}
        </NavLink>
      ))}
      <div className="mt-auto space-y-1 border-t border-slate-200 pt-2 dark:border-slate-800">
        <NavLink to="/account" className={linkClass}>
          <UserCircle size={18} aria-hidden />
          My account
        </NavLink>
        <button onClick={logout} className={linkClass({ isActive: false })}>
          <LogOut size={18} aria-hidden />
          Sign out
        </button>
        <ThemeSwitcher className="mx-1 mt-2" />
        <p className="truncate px-3 pt-1 text-xs text-slate-500">
          {user.display_name || user.name}
          {user.is_admin && " · admin"}
        </p>
      </div>
    </nav>
  );
}

function Shell() {
  const user = useUser();
  const location = useLocation();
  const [open, setOpen] = useState(false);

  // Close the phone menu after navigating, and with Escape.
  useEffect(() => setOpen(false), [location.pathname, location.search]);
  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && setOpen(false);
    window.addEventListener("keydown", onKey);
    document.body.style.overflow = "hidden"; // no background scrolling behind the menu
    return () => {
      window.removeEventListener("keydown", onKey);
      document.body.style.overflow = "";
    };
  }, [open]);

  return (
    <div className="flex min-h-screen flex-col md:flex-row">
      {/* Phone and small tablet: top bar with a menu button. */}
      <header className="sticky top-0 z-30 flex h-14 items-center justify-between border-b border-slate-200 bg-white px-4 md:hidden dark:border-slate-800 dark:bg-slate-900">
        <Logo size={28} />
        <button
          onClick={() => setOpen(true)}
          aria-label="Open menu"
          aria-expanded={open}
          aria-controls="mobile-menu"
          className="-mr-2 flex h-11 w-11 items-center justify-center rounded-md text-slate-600 hover:bg-slate-100 dark:text-slate-300 dark:hover:bg-slate-800"
        >
          <Menu size={22} />
        </button>
      </header>
      {open && (
        <div className="fixed inset-0 z-40 md:hidden" id="mobile-menu">
          <button aria-label="Close menu" className="absolute inset-0 h-full w-full bg-black/40" onClick={() => setOpen(false)} />
          <aside className="absolute inset-y-0 left-0 flex w-72 max-w-[85vw] flex-col bg-white shadow-xl dark:bg-slate-900">
            <div className="flex h-14 items-center justify-between px-4">
              <Logo size={28} />
              <button
                onClick={() => setOpen(false)}
                aria-label="Close menu"
                className="-mr-2 flex h-11 w-11 items-center justify-center rounded-md text-slate-600 hover:bg-slate-100 dark:text-slate-300 dark:hover:bg-slate-800"
              >
                <X size={22} />
              </button>
            </div>
            <NavLinks />
          </aside>
        </div>
      )}

      {/* Desktop: fixed sidebar. */}
      <aside className="sticky top-0 hidden h-screen w-60 shrink-0 flex-col border-r border-slate-200 bg-white md:flex dark:border-slate-800 dark:bg-slate-900">
        <div className="px-4 py-4 text-lg">
          <Logo size={30} />
        </div>
        <NavLinks />
      </aside>

      <main className="min-w-0 flex-1 p-4 md:p-8">{user.is_admin ? <Outlet /> : <Account />}</main>
    </div>
  );
}

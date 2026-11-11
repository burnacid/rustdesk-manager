import { createBrowserRouter } from "react-router";
import { Layout } from "./components/Layout";
import { Placeholder } from "./pages/Placeholder";
import { Dashboard } from "./pages/Dashboard";
import { Devices } from "./pages/Devices";
import { DeviceDetail } from "./pages/DeviceDetail";
import { DeviceGroups } from "./pages/DeviceGroups";
import { Audit } from "./pages/Audit";
import { Users } from "./pages/Users";
import { AddressBooks } from "./pages/AddressBooks";
import { AddressBookDetail } from "./pages/AddressBookDetail";
import { Strategies } from "./pages/Strategies";
import { Settings } from "./pages/Settings";
import { Recordings } from "./pages/Recordings";
import { Account } from "./pages/Account";

const soon = (title: string, milestone: number) => <Placeholder title={title} milestone={milestone} />;

export const router = createBrowserRouter([
  {
    path: "/",
    element: <Layout />,
    children: [
      { index: true, element: <Dashboard /> },
      { path: "devices", element: <Devices /> },
      { path: "devices/:id", element: <DeviceDetail /> },
      { path: "device-groups", element: <DeviceGroups /> },
      { path: "audit", element: <Audit /> },
      { path: "users", element: <Users /> },
      { path: "address-books", element: <AddressBooks /> },
      { path: "address-books/:id", element: <AddressBookDetail /> },
      { path: "strategies", element: <Strategies /> },
      { path: "recordings", element: <Recordings /> },
      { path: "settings", element: <Settings /> },
      { path: "account", element: <Account /> },
      { path: "*", element: soon("Page not found", 0) },
    ],
  },
]);

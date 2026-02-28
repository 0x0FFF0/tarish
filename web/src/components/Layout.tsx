import { NavLink, Outlet } from "react-router-dom"
import { cn } from "@/lib/utils"
import { LayoutDashboard, Server } from "lucide-react"

const navItems = [
  { to: "/", label: "Dashboard", icon: LayoutDashboard },
  { to: "/miners", label: "Miners", icon: Server },
]

export default function Layout() {
  return (
    <div className="min-h-screen">
      <header className="sticky top-0 z-50 border-b border-border bg-background/80 backdrop-blur-sm">
        <div className="mx-auto flex h-14 max-w-7xl items-center justify-between px-4">
          <div className="flex items-center gap-6">
            <span className="text-lg font-bold tracking-tight">
              <span className="text-primary">tarish</span>
              <span className="text-muted-foreground font-normal ml-1 text-sm">dashboard</span>
            </span>
            <nav className="flex items-center gap-1">
              {navItems.map(item => (
                <NavLink
                  key={item.to}
                  to={item.to}
                  end={item.to === "/"}
                  className={({ isActive }) =>
                    cn(
                      "flex items-center gap-2 rounded-md px-3 py-1.5 text-sm font-medium transition-colors",
                      isActive
                        ? "bg-secondary text-foreground"
                        : "text-muted-foreground hover:bg-secondary/50 hover:text-foreground"
                    )
                  }
                >
                  <item.icon className="h-4 w-4" />
                  {item.label}
                </NavLink>
              ))}
            </nav>
          </div>
        </div>
      </header>
      <main className="mx-auto max-w-7xl p-4 md:p-6">
        <Outlet />
      </main>
    </div>
  )
}

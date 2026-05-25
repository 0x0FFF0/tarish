import { BrowserRouter, Routes, Route } from "react-router-dom"
import Layout from "@/components/Layout"
import Dashboard from "@/pages/Dashboard"
import Miners from "@/pages/Miners"
import MinerDetail from "@/pages/MinerDetail"
import Guides from "@/pages/Guides"
import Settings from "@/pages/Settings"

export default function App() {
  return (
    <BrowserRouter>
      <Routes>
        <Route element={<Layout />}>
          <Route path="/" element={<Dashboard />} />
          <Route path="/miners" element={<Miners />} />
          <Route path="/miners/:id" element={<MinerDetail />} />
          <Route path="/guides" element={<Guides />} />
          <Route path="/settings" element={<Settings />} />
        </Route>
      </Routes>
    </BrowserRouter>
  )
}

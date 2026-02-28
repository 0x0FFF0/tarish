import { BrowserRouter, Routes, Route } from "react-router-dom"
import Layout from "@/components/Layout"
import Dashboard from "@/pages/Dashboard"
import Miners from "@/pages/Miners"
import MinerDetail from "@/pages/MinerDetail"

export default function App() {
  return (
    <BrowserRouter>
      <Routes>
        <Route element={<Layout />}>
          <Route path="/" element={<Dashboard />} />
          <Route path="/miners" element={<Miners />} />
          <Route path="/miners/:id" element={<MinerDetail />} />
        </Route>
      </Routes>
    </BrowserRouter>
  )
}

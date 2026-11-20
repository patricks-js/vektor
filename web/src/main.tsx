import { createRoot } from 'react-dom/client'
import './styles/global.css'
import { App } from './app/App'

const root = document.getElementById('root')
if (!root) throw new Error('Vektor root element missing')
createRoot(root).render(<App />)

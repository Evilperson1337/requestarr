import type { Metadata } from "next";
import "./globals.css";
export const metadata:Metadata={title:"Requestarr Setup Wizard",description:"Interactive first-run setup experience for Requestarr.",other:{"codex-preview":"development"}};
export default function RootLayout({children}:Readonly<{children:React.ReactNode}>){return <html lang="en"><body>{children}</body></html>}

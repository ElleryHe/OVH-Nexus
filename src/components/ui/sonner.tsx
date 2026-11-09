
"use client"

import { useTheme } from "@/context/ThemeContext"
import { Toaster as Sonner } from "sonner"

type ToasterProps = React.ComponentProps<typeof Sonner>

const Toaster = ({ ...props }: ToasterProps) => {
  const { resolvedTheme } = useTheme()

  return (
    <Sonner
      theme={resolvedTheme}
      position="top-right"
      className="toaster group"
      toastOptions={{
        classNames: {
          toast:
            "!bg-cyber-surface !text-cyber-text !border !border-cyber-grid !shadow-xl",
          description: "!text-cyber-muted !text-sm",
          actionButton:
            "!bg-cyber-accent !text-white !border !border-cyber-accent hover:!bg-cyber-accent/80",
          cancelButton:
            "!bg-cyber-dark !text-cyber-muted !border !border-cyber-grid hover:!bg-cyber-grid/40",
          error: "!bg-red-500/10 !border-red-500/40 !text-red-500",
          success: "!bg-emerald-500/10 !border-emerald-500/40 !text-emerald-600 dark:!text-emerald-400",
          warning: "!bg-amber-500/10 !border-amber-500/40 !text-amber-600 dark:!text-amber-400",
          info: "!bg-cyber-accent/10 !border-cyber-accent/40 !text-cyber-accent",
        },
        style: {
          background: 'rgb(var(--ovh-surface))',
          color: 'rgb(var(--ovh-text))',
          border: '1px solid rgb(var(--ovh-grid))',
        },
      }}
      {...props}
    />
  )
}

export { Toaster, toast } from "sonner"

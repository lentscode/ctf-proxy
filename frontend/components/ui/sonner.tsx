import { Toaster as Sonner, type ToasterProps } from "sonner";

// Toaster applies the dashboard's restrained dark visual language to shadcn's Sonner primitive.
export function Toaster(props: ToasterProps) {
  return (
    <Sonner
      closeButton
      position="bottom-right"
      theme="dark"
      toastOptions={{
        classNames: {
          toast:
            "group rounded-md border border-zinc-700 bg-zinc-900 px-4 py-3 text-zinc-100 shadow-xl shadow-black/35",
          title: "text-sm font-semibold text-zinc-100",
          description: "text-sm leading-relaxed text-zinc-400",
          actionButton: "bg-white text-black hover:bg-zinc-200",
          cancelButton: "bg-zinc-800 text-zinc-200 hover:bg-zinc-700",
          closeButton:
            "border-zinc-600 bg-zinc-900 text-zinc-300 hover:bg-zinc-800 hover:text-zinc-100",
          success: "!border-emerald-800 !bg-emerald-950 !text-emerald-100",
          error: "!border-red-800 !bg-red-950 !text-red-100",
        },
      }}
      {...props}
    />
  );
}

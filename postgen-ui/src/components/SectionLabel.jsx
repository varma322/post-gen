export default function SectionLabel({ icon, children }) {
  return (
    <div className="flex items-center gap-2 mb-4">
      {icon && <span className="material-symbols-outlined text-primary">{icon}</span>}
      <h2 className="text-xl font-headline font-semibold text-on-surface">{children}</h2>
    </div>
  );
}

import React from 'react';
import { useLocale, type Locale } from '@/context/LocaleContext';
import { formatDatacenter } from '@/config/datacenters';

export interface DCLabelProps {
  code: string;
  mode?: 'compact' | 'full';
  locale?: Locale;
  className?: string;
}

export const DCLabel: React.FC<DCLabelProps> = ({
  code,
  mode = 'compact',
  locale: localeProp,
  className = '',
}) => {
  const { locale: contextLocale } = useLocale();
  const activeLocale = localeProp || contextLocale;
  const fullLabel = formatDatacenter(code, activeLocale, 'full');
  const displayLabel =
    mode === 'full' ? fullLabel : formatDatacenter(code, activeLocale, 'compact');

  return (
    <span
      title={fullLabel}
      className={`inline-flex items-center gap-1 ${className}`.trim()}
    >
      {displayLabel}
    </span>
  );
};

export default DCLabel;

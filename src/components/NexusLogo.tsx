import React from 'react';

interface NexusLogoProps {
  size?: number;
  className?: string;
}

/**
 * OVH Nexus Brand Mark
 * Renders the frameless italic translucent blue Liquid Glass "N" emblem.
 */
export const NexusLogo: React.FC<NexusLogoProps> = ({ size = 36, className = '' }) => {
  return (
    <img
      src="/logo.png"
      width={size}
      height={size}
      alt="OVH Nexus Logo"
      draggable={false}
      className={`shrink-0 select-none object-contain ${className}`}
    />
  );
};

export default NexusLogo;

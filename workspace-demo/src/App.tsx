import { useEffect, useState } from 'react';
import { TabType, VpnLocation } from './types';
import { VPN_LOCATIONS } from './data/seed';
import { applyAccentPalette, readStoredTheme, writeStoredTheme } from './data/themes';
import { Navbar } from './components/Navbar';
import { BrowserView } from './components/BrowserView';
import { WorkflowBoard } from './components/WorkflowBoard';
import { CollaborativeDocs } from './components/CollaborativeDocs';
import { TeamHub } from './components/TeamHub';
import { VpnHub } from './components/VpnHub';
import { ThemeCustomizer } from './components/ThemeCustomizer';

export default function App() {
  const storedTheme = readStoredTheme();
  const [activeTab, setActiveTab] = useState<TabType>('browser');
  const [isDarkMode, setIsDarkMode] = useState<boolean>(true);
  const [vpnEnabled, setVpnEnabled] = useState<boolean>(true);
  const [activeVpnLocation, setActiveVpnLocation] = useState<VpnLocation>(VPN_LOCATIONS[0]);

  // Theme Customization States
  const [accentColor, setAccentColor] = useState<string>(storedTheme.accentColor);
  const [bgTheme, setBgTheme] = useState<string>(storedTheme.bgTheme);
  const [themeModalOpen, setThemeModalOpen] = useState<boolean>(false);

  useEffect(() => {
    applyAccentPalette(accentColor);
  }, [accentColor]);

  useEffect(() => {
    writeStoredTheme({ accentColor, bgTheme });
  }, [accentColor, bgTheme]);

  useEffect(() => {
    if (bgTheme === 'arctic') {
      setIsDarkMode(false);
      document.documentElement.classList.remove('dark');
    } else {
      setIsDarkMode(true);
      document.documentElement.classList.add('dark');
    }
  }, [bgTheme]);

  const getBgClass = () => {
    switch (bgTheme) {
      case 'arctic': return 'bg-slate-50 text-slate-900';
      case 'navy': return 'bg-[#0b1329] text-slate-100';
      case 'obsidian': return 'bg-black text-slate-100';
      default: return 'bg-slate-950 text-slate-100';
    }
  };

  return (
    <div className={`min-h-screen ${getBgClass()} font-sans transition-colors`}>
      <Navbar
        activeTab={activeTab}
        setActiveTab={setActiveTab}
        isDarkMode={isDarkMode}
        setIsDarkMode={(dark) => {
          setIsDarkMode(dark);
          if (dark) setBgTheme('midnight');
          else setBgTheme('arctic');
        }}
        vpnEnabled={vpnEnabled}
        activeVpnCity={activeVpnLocation.city}
        onOpenThemeModal={() => setThemeModalOpen(true)}
      />

      <main>
        {activeTab === 'browser' && (
          <BrowserView
            vpnEnabled={vpnEnabled}
            setVpnEnabled={setVpnEnabled}
            vpnLocations={VPN_LOCATIONS}
            activeVpnLocation={activeVpnLocation}
            setActiveVpnLocation={setActiveVpnLocation}
          />
        )}

        {activeTab === 'workflow' && <WorkflowBoard />}

        {activeTab === 'docs' && <CollaborativeDocs />}

        {activeTab === 'team' && (
          <TeamHub activeVpnLocation={activeVpnLocation} />
        )}

        {activeTab === 'vpn' && (
          <VpnHub
            vpnEnabled={vpnEnabled}
            setVpnEnabled={setVpnEnabled}
            vpnLocations={VPN_LOCATIONS}
            activeVpnLocation={activeVpnLocation}
            setActiveVpnLocation={setActiveVpnLocation}
          />
        )}
      </main>

      <ThemeCustomizer
        isOpen={themeModalOpen}
        onClose={() => setThemeModalOpen(false)}
        accentColor={accentColor}
        setAccentColor={setAccentColor}
        bgTheme={bgTheme}
        setBgTheme={setBgTheme}
      />
    </div>
  );
}

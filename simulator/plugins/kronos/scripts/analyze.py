import json
import pandas as pd
import matplotlib.pyplot as plt
from datetime import datetime
import matplotlib.dates as mdates
from matplotlib.ticker import MaxNLocator

def load_metrics(file_path):
    try:
        with open(file_path, 'r') as f:
            return json.load(f)
    except Exception as e:
        print(f"Error loading metrics: {e}")
        return []

def create_simple_plots(metrics):
    if not metrics:
        print("No metrics available to plot")
        return

    df = pd.DataFrame(metrics)
    df['timestamp'] = pd.to_datetime(df['ts'])
    
    # Set style for better looking plots
    plt.style.use('default')
    plt.rcParams['figure.facecolor'] = 'white'
    plt.rcParams['axes.facecolor'] = 'white'
    plt.rcParams['axes.grid'] = True
    plt.rcParams['grid.alpha'] = 0.3
    plt.rcParams['grid.linestyle'] = '--'
    
    # 1. Node Performance Overview
    plt.figure(figsize=(14, 8))
    for node in df['node'].unique():
        node_data = df[df['node'] == node]
        plt.plot(node_data['timestamp'], node_data['score'], 
                marker='o', linestyle='-', linewidth=2, 
                markersize=6, label=f'{node} (Score)')
    
    plt.title('Node Performance Over Time', fontsize=16, pad=20)
    plt.xlabel('Time', fontsize=12)
    plt.ylabel('Scheduler Score', fontsize=12)
    plt.legend(bbox_to_anchor=(1.05, 1), loc='upper left')
    plt.xticks(rotation=45)
    plt.tight_layout()
    plt.savefig('node_performance.png', bbox_inches='tight', dpi=300)
    print("Created 'node_performance.png'")
    
    # 2. Energy Efficiency Comparison
    plt.figure(figsize=(14, 8))
    avg_energy = df.groupby('node')['watts'].mean().sort_values()
    colors = ['#4CAF50', '#2196F3', '#FFC107']  # Green, Blue, Yellow
    
    ax = avg_energy.plot(kind='bar', color=colors, alpha=0.7)
    plt.title('Average Energy Usage by Node', fontsize=16, pad=20)
    plt.xlabel('Node', fontsize=12)
    plt.ylabel('Average Power (Watts)', fontsize=12)
    
    # Add value labels on top of bars
    for i, v in enumerate(avg_energy):
        ax.text(i, v + 1, f'{v:.1f}W', ha='center')
    
    plt.tight_layout()
    plt.savefig('energy_efficiency.png', bbox_inches='tight', dpi=300)
    print("Created 'energy_efficiency.png'")
    
    # 3. Scheduling Decisions
    plt.figure(figsize=(14, 8))
    scatter = plt.scatter(df['watts'], df['score'], 
                         c=df['wq'], 
                         cmap='viridis',
                         s=100, 
                         alpha=0.7)
    
    plt.colorbar(scatter, label='Queue Delay (seconds)')
    plt.title('Scheduling Decisions: Energy vs Performance', fontsize=16, pad=20)
    plt.xlabel('Power Consumption (Watts)', fontsize=12)
    plt.ylabel('Scheduler Score', fontsize=12)
    
    # Add node labels for better readability
    for i, txt in enumerate(df['node']):
        plt.annotate(txt, (df['watts'].iloc[i], df['score'].iloc[i]), 
                    textcoords="offset points", 
                    xytext=(0,5), 
                    ha='center',
                    fontsize=8)
    
    # Add reference lines
    plt.axhline(y=50, color='r', linestyle='--', alpha=0.5, label='Score Threshold')
    plt.axvline(x=df['watts'].median(), color='g', linestyle='--', alpha=0.5, label='Median Power')
    
    plt.legend()
    plt.tight_layout()
    plt.savefig('scheduling_decisions.png', bbox_inches='tight', dpi=300)
    print("Created 'scheduling_decisions.png'")
    
    # 4. Success Rate by Job Type
    if 'jobType' in df.columns:
        plt.figure(figsize=(10, 6))
        success_rate = df[df['scheduled']].groupby('jobType').size() / df.groupby('jobType').size() * 100
        ax = success_rate.plot(kind='bar', color=['#4CAF50', '#2196F3'])
        plt.title('Scheduling Success Rate by Job Type', fontsize=16, pad=20)
        plt.xlabel('Job Type', fontsize=12)
        plt.ylabel('Success Rate (%)', fontsize=12)
        plt.ylim(0, 110)
        
        # Add percentage labels
        for i, v in enumerate(success_rate):
            ax.text(i, v + 2, f'{v:.1f}%', ha='center')
            
        plt.tight_layout()
        plt.savefig('success_rate.png', bbox_inches='tight', dpi=300)
        print("Created 'success_rate.png'")

def main():
    metrics = load_metrics('kronos-metrics.json')
    if metrics:
        print(f"Analyzing {len(metrics)} scheduling decisions...")
        create_simple_plots(metrics)
        print("\nAnalysis complete! Check the generated images.")
    else:
        print("No metrics found or error loading metrics file.")

if __name__ == "__main__":
    main()